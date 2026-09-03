//! RFC 1123 的 GMT 时间串，OSS V1 签名的 Date 头要用。
//!
//! 自己算是因为构建环境里没有 chrono/time crate。日期换算用的是
//! Howard Hinnant 的 civil_from_days——一个被广泛引用、有闭式解的算法，
//! 比自己数闰年可靠得多。

const DAYS: [&str; 7] = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS: [&str; 12] = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

/// 把 Unix 秒转成 "Wed, 05 Sep 2012 23:00:00 GMT"。
pub fn format_http_date(unix_secs: i64) -> String {
    let days = unix_secs.div_euclid(86_400);
    let secs_of_day = unix_secs.rem_euclid(86_400);
    let (y, m, d) = civil_from_days(days);

    // 1970-01-01 是星期四。
    let weekday = (days + 4).rem_euclid(7) as usize;

    format!(
        "{}, {:02} {} {} {:02}:{:02}:{:02} GMT",
        DAYS[weekday],
        d,
        MONTHS[(m - 1) as usize],
        y,
        secs_of_day / 3600,
        (secs_of_day % 3600) / 60,
        secs_of_day % 60
    )
}

/// days 是 1970-01-01 以来的天数，返回 (年, 月 1..12, 日 1..31)。
fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097; // [0, 146096]
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365; // [0, 399]
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100); // [0, 365]
    let mp = (5 * doy + 2) / 153; // [0, 11]
    let d = doy - (153 * mp + 2) / 5 + 1; // [1, 31]
    let m = if mp < 10 { mp + 3 } else { mp - 9 }; // [1, 12]
    (if m <= 2 { y + 1 } else { y }, m, d)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_timestamps() {
        assert_eq!(
            format_http_date(1_346_886_000),
            "Wed, 05 Sep 2012 23:00:00 GMT"
        );
        assert_eq!(format_http_date(0), "Thu, 01 Jan 1970 00:00:00 GMT");
        // 闰日：这一天算错的话，每四年有一天所有签名全挂。
        assert_eq!(
            format_http_date(1_709_164_800),
            "Thu, 29 Feb 2024 00:00:00 GMT"
        );
        // 世纪年是闰年的特例（2000 年能被 400 整除）。
        assert_eq!(
            format_http_date(951_782_400),
            "Tue, 29 Feb 2000 00:00:00 GMT"
        );
        assert_eq!(
            format_http_date(1_735_689_599),
            "Tue, 31 Dec 2024 23:59:59 GMT"
        );
    }
}
