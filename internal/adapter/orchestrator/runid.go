package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
)

// RunIDGenerator produces run ids in the shape api/openapi.yaml documents:
// "run-" followed by 16 hex characters, e.g. "run-9e67931d5c38024e"，以及
// 同样形状的会话 id（"sess-" 前缀）。
type RunIDGenerator struct{}

func NewRunIDGenerator() RunIDGenerator { return RunIDGenerator{} }

func (RunIDGenerator) NewRunID() (string, error) { return randomID("run-") }

// NewSessionID 产生一段对话的 id。一段会话串起多次运行——用户连着发的每
// 条消息各是一次运行，但共享同一个 ADK 会话，模型因此看得到上文。
func (RunIDGenerator) NewSessionID() (string, error) { return randomID("sess-") }

func randomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}
