use prometheus::{HistogramVec, IntCounterVec, register_histogram_vec, register_int_counter_vec};

lazy_static::lazy_static! {
    pub static ref CACHE_REQUESTS: IntCounterVec = register_int_counter_vec!(
        "cache_requests_total", "Total cache requests", &["status"]
    ).unwrap();
    pub static ref CACHE_LATENCY: HistogramVec = register_histogram_vec!(
        "cache_latency_seconds", "Cache operation latency", &["operation"]
    ).unwrap();
    pub static ref TOKENS_SAVED: IntCounterVec = register_int_counter_vec!(
        "cache_tokens_saved_total", "Tokens saved by cache", &["model"]
    ).unwrap();
}
