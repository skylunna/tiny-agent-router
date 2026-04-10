use config::{Config, File, FileFormat};
use serde::Deserialize;

#[derive(Debug, Deserialize, Clone)]
pub struct CacheConfig {
    pub server: ServerConfig,
    pub embedding: EmbeddingConfig,
    pub storage: StorageConfig,
    pub similarity: SimilarityConfig,
}

#[derive(Debug, Deserialize, Clone)]
pub struct ServerConfig {
    pub addr: String, // "[::1]:50051"
}

#[derive(Debug, Deserialize, Clone)]
pub struct EmbeddingConfig {
    pub provider: String,   // "ollama"
    pub model: String,      // "nomic-embed-text"
    pub ollama_url: String, // "http://localhost:11434"
    pub dimensions: usize,  // 768 or 1024
}

#[derive(Debug, Deserialize, Clone)]
pub struct StorageConfig {
    pub db_path: String,  // "cache.db"
    pub ttl_seconds: i64, // 86400 = 1 day
}

#[derive(Debug, Deserialize, Clone)]
pub struct SimilarityConfig {
    pub threshold: f32, // 0.92
}

pub fn load_config(path: &str) -> Result<CacheConfig, Box<dyn std::error::Error>> {
    let cfg = Config::builder()
        .add_source(File::with_name(path).format(FileFormat::Yaml))
        .build()?;

    Ok(cfg.try_deserialize()?)
}
