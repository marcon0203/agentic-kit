package adk

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// countingAgent 每次被调用就吐一条事件，用来数它跑了几次。
func countingAgent(t *testing.T, name string, runs *int) agent.Agent {
	t.Helper()
	return mockLLMAgent(t, name, func(ic agent.InvocationContext) *session.Event {
		*runs++
		return finalTextEvent(ic, "ok", nil)
	})
}

// drain 通过真的 ADK runner 跑一次这个 agent，返回它是否失败。
//
// 不能直接 a.Run(nil)：ADK 的 agent 包在 Run 外面还有一层，要有真的
// InvocationContext 才能跑——这也更接近线上，转交计数壳本来就是活在
// runner 里的。
func drain(t *testing.T, a agent.Agent) error {
	t.Helper()
	rn, err := runner.New(runner.Config{
		AppName: "test", Agent: a, SessionService: session.InMemoryService(), AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range rn.Run(ctx, "u", "s", genai.NewContentFromText("go", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return err
		}
	}
	return nil
}

// 转交次数封顶。模型自己不会喊停，而 ADK 的 transfer 没有内建上限，所以
// 这一层必须由我们加——否则两个 Agent 互相推诿会一直烧 token 到运行的墙钟
// 上限。
func TestCountHandoffs_FailsOnceTheCapIsExceeded(t *testing.T) {
	runs := 0
	counter := NewHandoffCounter(3)
	wrapped, err := CountHandoffs("worker", countingAgent(t, "worker", &runs), counter)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := drain(t, wrapped); err != nil {
			t.Fatalf("第 %d 次转交不该失败：%v", i, err)
		}
	}
	if runs != 3 {
		t.Fatalf("上限内的每一次都应当真的执行，实际跑了 %d 次", runs)
	}

	err = drain(t, wrapped)
	if err == nil {
		t.Fatal("第 4 次转交超过上限 3，必须失败")
	}
	if !strings.Contains(err.Error(), "转交次数超过上限") {
		t.Fatalf("错误信息要说清楚发生了什么，实际是：%v", err)
	}
	if runs != 3 {
		t.Fatalf("超限那次不该再执行候选，实际跑了 %d 次", runs)
	}
}

// 计数器是**整棵树共享**的：上限约束的是"这次运行一共转交了多少次"，
// 不是"某个候选被调用了多少次"。两个候选轮流被叫，一样要撞上限。
func TestCountHandoffs_CapIsSharedAcrossCandidates(t *testing.T) {
	var aRuns, bRuns int
	counter := NewHandoffCounter(2)
	first, _ := CountHandoffs("a", countingAgent(t, "a", &aRuns), counter)
	second, _ := CountHandoffs("b", countingAgent(t, "b", &bRuns), counter)

	if err := drain(t, first); err != nil {
		t.Fatalf("第 1 次：%v", err)
	}
	if err := drain(t, second); err != nil {
		t.Fatalf("第 2 次：%v", err)
	}
	if err := drain(t, first); err == nil {
		t.Fatal("两个候选共用一个上限，第 3 次必须失败")
	}
}

func TestNewHandoffCounter_ZeroMeansTheDefault(t *testing.T) {
	if got := NewHandoffCounter(0).max; got != defaultMaxHandoffs {
		t.Fatalf("0 应当落到默认值 %d，实际 %d", defaultMaxHandoffs, got)
	}
}

// 没有候选的 router 编译不出来：那样的 Bundle 里路由器无人可交，
// ADK 也不会给它挂 transfer_to_agent 工具，跑起来就是个普通单体 Agent——
// 与其让用户以为自己在用 router，不如在编译期说清楚。
func TestCompileRouter_RejectsARouterWithNoCandidates(t *testing.T) {
	runs := 0
	_, err := CompileRouter(RouterCompileOptions{
		BundleRef: "b1", Router: countingAgent(t, "supervisor", &runs), RouterNode: "supervisor",
	})
	if err == nil || !strings.Contains(err.Error(), "no candidate") {
		t.Fatalf("期望「没有候选」的编译错误，实际 %v", err)
	}
}

func TestCompileRouter_RejectsAMissingRouter(t *testing.T) {
	_, err := CompileRouter(RouterCompileOptions{
		BundleRef: "b1", RouterNode: "supervisor", Candidates: []string{"w"},
	})
	if err == nil || !strings.Contains(err.Error(), "was not compiled") {
		t.Fatalf("路由节点没编译出来时要明确报错，实际 %v", err)
	}
}
