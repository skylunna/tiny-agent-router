// cache-service/src/embedding.rs
use ollama_rs::{
    Ollama,
    generation::embeddings::request::{EmbeddingsInput, GenerateEmbeddingsRequest}, // ✅ 关键：导入 EmbeddingsInput
};
use tracing::info;

pub struct EmbeddingService {
    client: Ollama,
    model: String,
    timeout_secs: u64,
}

impl EmbeddingService {
    pub fn new(ollama_url: &str, model: &str, timeout_secs: u64) -> Self {
        // ollama-rs v0.2+ 要求分离 host 和 port
        let host = if ollama_url.starts_with("http") {
            ollama_url.to_string()
        } else {
            format!("http://{}", ollama_url)
        };

        Self {
            client: Ollama::new(host, 11434),
            model: model.to_string(),
            timeout_secs,
        }
    }

    pub async fn generate(&self, text: &str) -> Result<Vec<f32>, Box<dyn std::error::Error>> {
        // ✅ 构造请求：用 EmbeddingsInput::Single 包装文本
        let request = GenerateEmbeddingsRequest::new(
            self.model.clone(),
            EmbeddingsInput::Single(text.to_string()), // ✅ 关键修复
        );

        // ✅ 调用：只传一个 request 参数
        let response = tokio::time::timeout(
            std::time::Duration::from_secs(self.timeout_secs),
            self.client.generate_embeddings(request),
        )
        .await??; // 第一个 ? 解超时，第二个 ? 解业务错误

        // ✅ 解析响应：ollama-rs v0.2 返回 Vec<f32>，直接克隆即可
        if let Some(embedding) = response.embeddings.first() {
            info!("✅ Generated embedding, dims={}", embedding.len());
            Ok(embedding.clone())
        } else {
            Err("Empty embedding response from Ollama".into())
        }
    }
}
