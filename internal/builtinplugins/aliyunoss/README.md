# 阿里云 OSS 插件

读写一个 OSS 存储桶：上传、下载、列举、生成预签名链接。

## 凭据

用户在安装时自己填（清单的 `requires.config_schema` 声明要哪几项）：

| 键 | 说明 | 保密 |
|---|---|---|
| `endpoint` | 地域接入域名，如 `oss-cn-hangzhou.aliyuncs.com` | 否 |
| `bucket` | 存储桶名 | 否 |
| `access_key_id` | AccessKey ID，相当于用户名，不是秘密 | 否 |
| `access_key_secret` | AccessKey Secret | **是** |

`access_key_secret` 的键名里带 `secret`，宿主据此加密落库并从所有响应里抹
掉。这不是巧合——清单校验会拒绝"声明了 `secret: true` 但键名认不出是凭据"
的字段，否则那个字段会明文落库，而作者以为自己声明了保密。

装完之后在 组件广场 → 插件 → 配置 里可以看当前配置、换密钥。密钥的值不会
再显示出来，留空即表示不改。

建议用只授权了这一个存储桶的 RAM 子账号。

## 为什么签名代码是自己写的

构建环境的 cargo 缓存里没有 `hmac`/`sha1` crate，而 OSS V1 签名就要
HMAC-SHA1。所以 SHA-1、HMAC、RFC 1123 日期三样都在包内实现，各自用权威向量
钉住：

- SHA-1 → RFC 3174 / FIPS 180-1 向量，含 100 万个 `a` 的长输入和分片喂入
- HMAC-SHA1 → RFC 2202 向量，含超过一个分组的长密钥
- 日期 → 闰日、世纪闰年、跨年边界

待签串的**形状**（换行位置、CanonicalizedResource 拼法）另有专门的测试逐字
断言——这层是唯一可能写错的地方，错了对真实 bucket 就是清一色 403。

## 大文件

`oss_put_object` / `oss_get_object` 单次上限 5MB。沙箱有内存上限，而且内容
最终要经过模型的上下文。真正的大文件走 `oss_presign_url` 拿一个直传/直下链
接给用户，内容根本不必进对话。

## 构建

```
cargo test                                          # 含全部签名向量
cargo build --release --target wasm32-wasip1
cp target/wasm32-wasip1/release/aliyun_oss_plugin.wasm plugin.wasm
```

`plugin.wasm` 随二进制 embed，服务启动时由 `builtinplugins.SeedAll` 播种。
改了 Rust 源码记得重新构建并提交 `plugin.wasm`。
