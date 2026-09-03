//! 阿里云 OSS 插件：上传、下载、列举、生成预签名 URL。
//!
//! 凭据由用户在安装时填（清单的 requires.config_schema 声明要哪几项），宿主
//! 加密落库，调用时解密后经 Extism 的 config 下发到沙箱。插件不持久化任何
//! 东西，也不认识别的租户——一次调用读一次 config，用完就随实例一起销毁。
//!
//! 签名走 OSS V1（HMAC-SHA1）。SHA-1/HMAC/日期格式都在包内自己实现，因为构
//! 建环境没有对应的 crate；三者各有权威测试向量钉着（见各自的 tests）。

mod httpdate;
mod sha1;

use base64::{engine::general_purpose::STANDARD as B64, Engine as _};
use extism_pdk::*;
use serde::{Deserialize, Serialize};

use httpdate::format_http_date;
use sha1::hmac_sha1;

/// 单次传输的字节上限。
///
/// 沙箱有内存上限，而且这些内容最终要经过模型的上下文——真正的大文件应该走
/// oss_presign_url 给一个链接，而不是把几十兆塞进对话里。
const MAX_BYTES: usize = 5 * 1024 * 1024;

// ── 配置 ─────────────────────────────────────────────────────────────

struct OssConfig {
    endpoint: String,
    bucket: String,
    access_key_id: String,
    access_key_secret: String,
}

fn load_config() -> Result<OssConfig, Error> {
    let get = |k: &str| -> Result<String, Error> {
        match config::get(k)? {
            Some(v) if !v.trim().is_empty() => Ok(v.trim().to_string()),
            // 说清楚是"没配"而不是"调用失败"——用户该去插件的配置里补，不是
            // 去查 OSS 的状态。
            _ => Err(Error::msg(format!(
                "插件配置里缺少 {k}，去 组件广场 → 已安装 里补上"
            ))),
        }
    };
    Ok(OssConfig {
        endpoint: get("endpoint")?.trim_end_matches('/').to_string(),
        bucket: get("bucket")?,
        access_key_id: get("access_key_id")?,
        access_key_secret: get("access_key_secret")?,
    })
}

impl OssConfig {
    fn host(&self) -> String {
        format!("{}.{}", self.bucket, self.endpoint)
    }
    fn url(&self, key: &str, query: &str) -> String {
        let q = if query.is_empty() {
            String::new()
        } else {
            format!("?{query}")
        };
        format!("https://{}/{}{}", self.host(), encode_key(key), q)
    }
    /// CanonicalizedResource：/bucket/object，带上参与签名的子资源。
    fn canonical_resource(&self, key: &str, sub: &str) -> String {
        let base = if key.is_empty() {
            format!("/{}/", self.bucket)
        } else {
            format!("/{}/{}", self.bucket, key)
        };
        if sub.is_empty() {
            base
        } else {
            format!("{base}?{sub}")
        }
    }
}

/// 对象名按 path 段转义。斜杠要保留——OSS 的 key 用它表达目录层级；转义了
/// 就会变成一个名字里带斜杠的对象，和用户预期完全不同。
fn encode_key(key: &str) -> String {
    key.split('/')
        .map(percent_encode_segment)
        .collect::<Vec<_>>()
        .join("/")
}

fn percent_encode_segment(seg: &str) -> String {
    let mut out = String::with_capacity(seg.len());
    for b in seg.as_bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(*b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

fn urlencode_all(s: &str) -> String {
    percent_encode_segment(s)
}

// ── 签名 ─────────────────────────────────────────────────────────────

/// OSS V1 待签串（见阿里云"在 Header 中包含签名"）：
///
/// VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedOSSHeaders + CanonicalizedResource
fn sign(
    secret: &str,
    verb: &str,
    content_type: &str,
    date: &str,
    canonical_resource: &str,
) -> String {
    let string_to_sign = format!("{verb}\n\n{content_type}\n{date}\n{canonical_resource}");
    B64.encode(hmac_sha1(secret.as_bytes(), string_to_sign.as_bytes()))
}

fn now_unix() -> i64 {
    // WASI 的墙钟。取不到就退回 0——那样签出来的 Date 会被 OSS 判为时间偏
    // 差过大并明确拒绝，比默默发一个签不上的请求好。
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// 发一个签好名的请求。body 为 None 时不带正文。
fn signed_request(
    cfg: &OssConfig,
    verb: &str,
    key: &str,
    sub_resource: &str,
    query: &str,
    content_type: &str,
    body: Option<Vec<u8>>,
) -> Result<HttpResponse, Error> {
    let date = format_http_date(now_unix());
    let signature = sign(
        &cfg.access_key_secret,
        verb,
        content_type,
        &date,
        &cfg.canonical_resource(key, sub_resource),
    );

    let mut req = HttpRequest::new(cfg.url(key, query))
        .with_method(verb)
        .with_header("Host", cfg.host())
        .with_header("Date", date)
        .with_header(
            "Authorization",
            format!("OSS {}:{}", cfg.access_key_id, signature),
        );
    if !content_type.is_empty() {
        req = req.with_header("Content-Type", content_type);
    }
    Ok(http::request::<Vec<u8>>(&req, body)?)
}

/// OSS 出错时回的是 XML，整段塞进错误信息太吵。把 <Code> 和 <Message> 摘出
/// 来——"NoSuchKey: The specified key does not exist" 比一个 404 有用得多。
fn upstream_error(status: u16, body: &[u8]) -> Error {
    let text = String::from_utf8_lossy(body);
    let pick = |tag: &str| -> Option<String> {
        let open = format!("<{tag}>");
        let close = format!("</{tag}>");
        let s = text.find(&open)? + open.len();
        let e = text[s..].find(&close)? + s;
        Some(text[s..e].to_string())
    };
    match (pick("Code"), pick("Message")) {
        (Some(c), Some(m)) => Error::msg(format!("OSS 返回 {status} {c}：{m}")),
        _ => {
            let snippet: String = text.chars().take(300).collect();
            Error::msg(format!("OSS 返回 {status}：{snippet}"))
        }
    }
}

// ── 工具：上传 ───────────────────────────────────────────────────────

#[derive(Deserialize)]
struct PutInput {
    key: String,
    /// 文本内容。与 content_base64 二选一。
    #[serde(default)]
    content: Option<String>,
    /// 二进制内容（base64）。与 content 二选一。
    #[serde(default)]
    content_base64: Option<String>,
    #[serde(default)]
    content_type: Option<String>,
}

#[derive(Serialize)]
struct PutOutput {
    ok: bool,
    key: String,
    size: usize,
    url: String,
}

#[plugin_fn]
pub fn oss_put_object(Json(input): Json<PutInput>) -> FnResult<Json<PutOutput>> {
    let cfg = load_config()?;
    if input.key.trim().is_empty() {
        return Err(WithReturnCode::new(Error::msg("key 不能为空"), 1));
    }

    let bytes: Vec<u8> = match (&input.content, &input.content_base64) {
        (Some(_), Some(_)) => {
            return Err(WithReturnCode::new(
                Error::msg("content 和 content_base64 只能给一个"),
                1,
            ))
        }
        (Some(text), None) => text.as_bytes().to_vec(),
        (None, Some(b64)) => B64.decode(b64.trim()).map_err(|e| {
            WithReturnCode::new(
                Error::msg(format!("content_base64 不是合法的 base64：{e}")),
                1,
            )
        })?,
        (None, None) => {
            return Err(WithReturnCode::new(
                Error::msg("要上传的内容不能为空：给 content 或 content_base64"),
                1,
            ))
        }
    };
    if bytes.len() > MAX_BYTES {
        return Err(WithReturnCode::new(
            Error::msg(format!(
                "内容 {} 字节，超过单次 {} 字节的上限；大文件请用 oss_presign_url 拿一个直传链接",
                bytes.len(),
                MAX_BYTES
            )),
            1,
        ));
    }

    let content_type = input
        .content_type
        .clone()
        .unwrap_or_else(|| guess_content_type(&input.key).to_string());
    let size = bytes.len();

    let resp = signed_request(&cfg, "PUT", &input.key, "", "", &content_type, Some(bytes))?;
    if resp.status_code() / 100 != 2 {
        return Err(WithReturnCode::new(
            upstream_error(resp.status_code(), &resp.body()),
            1,
        ));
    }

    Ok(Json(PutOutput {
        ok: true,
        key: input.key.clone(),
        size,
        url: cfg.url(&input.key, ""),
    }))
}

// ── 工具：下载 ───────────────────────────────────────────────────────

#[derive(Deserialize)]
struct GetInput {
    key: String,
}

#[derive(Serialize)]
struct GetOutput {
    key: String,
    size: usize,
    /// 内容能按 UTF-8 解出来时给这个。
    #[serde(skip_serializing_if = "Option::is_none")]
    content: Option<String>,
    /// 二进制内容走 base64。
    #[serde(skip_serializing_if = "Option::is_none")]
    content_base64: Option<String>,
    is_text: bool,
}

#[plugin_fn]
pub fn oss_get_object(Json(input): Json<GetInput>) -> FnResult<Json<GetOutput>> {
    let cfg = load_config()?;
    if input.key.trim().is_empty() {
        return Err(WithReturnCode::new(Error::msg("key 不能为空"), 1));
    }

    let resp = signed_request(&cfg, "GET", &input.key, "", "", "", None)?;
    if resp.status_code() / 100 != 2 {
        return Err(WithReturnCode::new(
            upstream_error(resp.status_code(), &resp.body()),
            1,
        ));
    }

    let body = resp.body();
    if body.len() > MAX_BYTES {
        return Err(WithReturnCode::new(
            Error::msg(format!(
                "对象 {} 字节，超过单次 {} 字节的上限；用 oss_presign_url 拿一个下载链接",
                body.len(),
                MAX_BYTES
            )),
            1,
        ));
    }

    // 文本直接给模型看；二进制走 base64，别把乱码塞进上下文。
    let size = body.len();
    match String::from_utf8(body.clone()) {
        Ok(text) => Ok(Json(GetOutput {
            key: input.key,
            size,
            content: Some(text),
            content_base64: None,
            is_text: true,
        })),
        Err(_) => Ok(Json(GetOutput {
            key: input.key,
            size,
            content: None,
            content_base64: Some(B64.encode(&body)),
            is_text: false,
        })),
    }
}

// ── 工具：预签名 URL ─────────────────────────────────────────────────

#[derive(Deserialize)]
struct PresignInput {
    key: String,
    /// GET（下载）或 PUT（直传）。默认 GET。
    #[serde(default)]
    method: Option<String>,
    #[serde(default)]
    expires_seconds: Option<u32>,
}

#[derive(Serialize)]
struct PresignOutput {
    url: String,
    method: String,
    expires_at: String,
}

#[plugin_fn]
pub fn oss_presign_url(Json(input): Json<PresignInput>) -> FnResult<Json<PresignOutput>> {
    let cfg = load_config()?;
    if input.key.trim().is_empty() {
        return Err(WithReturnCode::new(Error::msg("key 不能为空"), 1));
    }
    let method = input.method.unwrap_or_else(|| "GET".into()).to_uppercase();
    if method != "GET" && method != "PUT" {
        return Err(WithReturnCode::new(
            Error::msg("method 只能是 GET 或 PUT"),
            1,
        ));
    }
    // 上限 7 天：更长的链接一旦泄露，回收的唯一办法是换密钥。
    let expires_in = input
        .expires_seconds
        .unwrap_or(3600)
        .clamp(60, 7 * 24 * 3600) as i64;
    let expires_at = now_unix() + expires_in;

    // 预签名的待签串把 Date 位置换成过期时间戳。
    let string_to_sign = format!(
        "{method}\n\n\n{expires_at}\n{}",
        cfg.canonical_resource(&input.key, "")
    );
    let signature = B64.encode(hmac_sha1(
        cfg.access_key_secret.as_bytes(),
        string_to_sign.as_bytes(),
    ));

    let query = format!(
        "OSSAccessKeyId={}&Expires={}&Signature={}",
        urlencode_all(&cfg.access_key_id),
        expires_at,
        urlencode_all(&signature)
    );

    Ok(Json(PresignOutput {
        url: cfg.url(&input.key, &query),
        method,
        expires_at: format_http_date(expires_at),
    }))
}

// ── 工具：列举 ───────────────────────────────────────────────────────

#[derive(Deserialize)]
struct ListInput {
    #[serde(default)]
    prefix: Option<String>,
    #[serde(default)]
    max_keys: Option<u32>,
}

#[derive(Serialize)]
struct ListItem {
    key: String,
    size: u64,
    last_modified: String,
}

#[derive(Serialize)]
struct ListOutput {
    items: Vec<ListItem>,
    truncated: bool,
}

#[plugin_fn]
pub fn oss_list_objects(Json(input): Json<ListInput>) -> FnResult<Json<ListOutput>> {
    let cfg = load_config()?;
    let max_keys = input.max_keys.unwrap_or(100).clamp(1, 1000);
    let prefix = input.prefix.unwrap_or_default();

    // ListObjectsV2。子资源要按字典序参与签名，list-type 在 max-keys 之后、
    // prefix 之前——顺序错了签名就对不上。
    let mut params = vec![format!("list-type=2"), format!("max-keys={max_keys}")];
    if !prefix.is_empty() {
        params.push(format!("prefix={}", urlencode_all(&prefix)));
    }
    params.sort();
    let query = params.join("&");

    let resp = signed_request(&cfg, "GET", "", &query, &query, "", None)?;
    if resp.status_code() / 100 != 2 {
        return Err(WithReturnCode::new(
            upstream_error(resp.status_code(), &resp.body()),
            1,
        ));
    }

    let xml = String::from_utf8_lossy(&resp.body()).to_string();
    let items = parse_list_xml(&xml);
    let truncated = xml.contains("<IsTruncated>true</IsTruncated>");
    Ok(Json(ListOutput { items, truncated }))
}

/// 从 ListObjects 的 XML 里摘出条目。这里不引 XML 解析库——响应结构固定且
/// 扁平，按标签切比拉一个解析器进 wasm 划算得多。
fn parse_list_xml(xml: &str) -> Vec<ListItem> {
    let mut out = Vec::new();
    for chunk in xml.split("<Contents>").skip(1) {
        let body = match chunk.find("</Contents>") {
            Some(i) => &chunk[..i],
            None => chunk,
        };
        let tag = |t: &str| -> Option<String> {
            let open = format!("<{t}>");
            let close = format!("</{t}>");
            let s = body.find(&open)? + open.len();
            let e = body[s..].find(&close)? + s;
            Some(body[s..e].to_string())
        };
        if let Some(key) = tag("Key") {
            out.push(ListItem {
                key,
                size: tag("Size").and_then(|s| s.parse().ok()).unwrap_or(0),
                last_modified: tag("LastModified").unwrap_or_default(),
            });
        }
    }
    out
}

fn guess_content_type(key: &str) -> &'static str {
    let lower = key.to_lowercase();
    let ext = lower.rsplit('.').next().unwrap_or("");
    match ext {
        "txt" | "log" | "md" => "text/plain; charset=utf-8",
        "json" => "application/json",
        "csv" => "text/csv; charset=utf-8",
        "html" | "htm" => "text/html; charset=utf-8",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "webp" => "image/webp",
        "svg" => "image/svg+xml",
        "pdf" => "application/pdf",
        "zip" => "application/zip",
        _ => "application/octet-stream",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg() -> OssConfig {
        OssConfig {
            endpoint: "oss-cn-hangzhou.aliyuncs.com".into(),
            bucket: "my-bucket".into(),
            access_key_id: "AKID".into(),
            access_key_secret: "SECRET".into(),
        }
    }

    /// 待签串的**形状**是这层唯一可能写错的东西——HMAC-SHA1 本身已经被
    /// sha1.rs 里的 RFC 2202 向量钉死了。所以这里断言拼出来的字符串逐字
    /// 相同，而不是去比一个没法独立核对的 base64：
    ///
    ///   VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedResource
    ///
    /// 我们不发 Content-MD5，也不发任何 x-oss-* 自定义头，所以第二行恒为
    /// 空、CanonicalizedOSSHeaders 整段不存在。少一个换行或多一个空格，对
    /// 真实 bucket 就是清一色 403 SignatureDoesNotMatch。
    #[test]
    fn string_to_sign_has_the_exact_documented_shape() {
        let built = format!(
            "{verb}\n\n{ct}\n{date}\n{res}",
            verb = "PUT",
            ct = "text/html",
            date = "Thu, 17 Nov 2005 18:49:58 GMT",
            res = "/oss-example/nelson"
        );
        assert_eq!(
            built,
            "PUT\n\ntext/html\nThu, 17 Nov 2005 18:49:58 GMT\n/oss-example/nelson"
        );
        // sign() 必须就是"对这个串做 HMAC-SHA1 再 base64"，没有别的动作。
        assert_eq!(
            sign(
                "OtxrzxIsfpFjA7SwPzILwy8Bw21TLhquhboDYROV",
                "PUT",
                "text/html",
                "Thu, 17 Nov 2005 18:49:58 GMT",
                "/oss-example/nelson",
            ),
            B64.encode(hmac_sha1(
                b"OtxrzxIsfpFjA7SwPzILwy8Bw21TLhquhboDYROV",
                built.as_bytes(),
            ))
        );
    }

    /// 预签名走的是另一套待签串：Date 的位置换成过期时间戳，Content-Type
    /// 也不参与。两者混掉的话链接一律签不上。
    #[test]
    fn presign_string_to_sign_uses_expiry_in_place_of_date() {
        let c = cfg();
        let built = format!(
            "GET\n\n\n{}\n{}",
            1_700_000_000i64,
            c.canonical_resource("a.txt", "")
        );
        assert_eq!(built, "GET\n\n\n1700000000\n/my-bucket/a.txt");
    }

    // CanonicalizedResource 拼错是签名失败最常见的原因。
    #[test]
    fn canonical_resource_shapes() {
        let c = cfg();
        assert_eq!(c.canonical_resource("a/b.txt", ""), "/my-bucket/a/b.txt");
        // 列举是对 bucket 本身的操作，key 为空时结尾要有斜杠。
        assert_eq!(
            c.canonical_resource("", "list-type=2"),
            "/my-bucket/?list-type=2"
        );
    }

    // key 里的斜杠是目录层级，必须原样保留；其余字符转义。
    #[test]
    fn key_encoding_keeps_slashes() {
        assert_eq!(
            encode_key("dir/sub/file name.txt"),
            "dir/sub/file%20name.txt"
        );
        assert_eq!(encode_key("中文.txt"), "%E4%B8%AD%E6%96%87.txt");
        assert_eq!(encode_key("a+b&c.txt"), "a%2Bb%26c.txt");
    }

    #[test]
    fn url_shape() {
        let c = cfg();
        assert_eq!(
            c.url("dir/a.txt", ""),
            "https://my-bucket.oss-cn-hangzhou.aliyuncs.com/dir/a.txt"
        );
        assert_eq!(
            c.url("a.txt", "Expires=1"),
            "https://my-bucket.oss-cn-hangzhou.aliyuncs.com/a.txt?Expires=1"
        );
    }

    // OSS 的错误是 XML，要摘成一句人能看懂的话。
    #[test]
    fn upstream_error_extracts_code_and_message() {
        let xml = br#"<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>"#;
        let e = upstream_error(404, xml);
        let s = e.to_string();
        assert!(s.contains("NoSuchKey"), "{s}");
        assert!(s.contains("The specified key does not exist"), "{s}");
    }

    #[test]
    fn list_xml_parsing() {
        let xml = r#"<ListBucketResult>
            <IsTruncated>false</IsTruncated>
            <Contents><Key>a.txt</Key><LastModified>2024-01-01T00:00:00.000Z</LastModified><Size>12</Size></Contents>
            <Contents><Key>dir/b.png</Key><LastModified>2024-02-02T00:00:00.000Z</LastModified><Size>345</Size></Contents>
        </ListBucketResult>"#;
        let items = parse_list_xml(xml);
        assert_eq!(items.len(), 2);
        assert_eq!(items[0].key, "a.txt");
        assert_eq!(items[0].size, 12);
        assert_eq!(items[1].key, "dir/b.png");
        assert_eq!(items[1].size, 345);
    }

    #[test]
    fn content_type_guessing() {
        assert_eq!(guess_content_type("a.json"), "application/json");
        assert_eq!(guess_content_type("dir/IMAGE.PNG"), "image/png");
        assert_eq!(guess_content_type("noext"), "application/octet-stream");
    }
}
