// Built-in chart renderer plugin (spec-20 §4.2 method A: an explicit
// tools[] call whose result a `ui` entry renders). Previously this plugin
// was renderer-only ("auto_render" method B — a ```chart fenced code block
// in the model's own reply got pattern-matched and handed to ui/chart.html
// once the whole message finished). That approach can't preserve message
// ordering when the model writes text both before and after the chart
// (the render event only fires after node.finished, so the chart always
// ends up positioned after ALL of the message's text, not where it
// actually belongs) — a real tool call doesn't have that problem, since
// node.tool_call.finished fires at its actual point in the event stream.
use extism_pdk::*;
use serde::{Deserialize, Serialize};

#[derive(Deserialize, Serialize)]
struct Dataset {
    label: String,
    data: Vec<f64>,
}

#[derive(Deserialize, Serialize)]
struct ChartSpec {
    #[serde(rename = "type")]
    chart_type: String,
    #[serde(default)]
    title: Option<String>,
    labels: Vec<String>,
    datasets: Vec<Dataset>,
}

/// render_chart does no real computation — the model's arguments are
/// already validated against the tool's own input_schema before this ever
/// runs. It exists purely so calling it is a real tool call with a real,
/// sequenced node.tool_call.finished event; echoing the same struct back
/// as the result is what the host hands straight to ui/chart.html's
/// render(), unchanged.
#[plugin_fn]
pub fn render_chart(input: Json<ChartSpec>) -> FnResult<Json<ChartSpec>> {
    let Json(spec) = input;
    if spec.labels.is_empty() {
        return Err(Error::msg("labels 不能为空").into());
    }
    for ds in &spec.datasets {
        if ds.data.len() != spec.labels.len() {
            return Err(Error::msg(format!(
                "dataset {:?} 的 data 长度（{}）和 labels 长度（{}）不一致",
                ds.label,
                ds.data.len(),
                spec.labels.len()
            ))
            .into());
        }
    }
    Ok(Json(spec))
}
