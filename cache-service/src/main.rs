mod embedding;
mod proto;
mod server;
mod storage;

use crate::embedding::EmbeddingService;
use crate::proto::cache::semantic_cache_server::SemanticCacheServer;
use crate::server::CacheService;
use crate::storage::CacheDB;
use std::sync::Arc;
use tonic::transport::Server;
use tracing::info;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // 初始化日志（显示 DEBUG 级别）
    tracing_subscriber::fmt()
        .with_env_filter("info,cache_service=debug")
        .init();

    let db_path = "cache.db";
    let ollama_url = "http://localhost:11434";
    let embedding_model = "nomic-embed-text";
    let similarity_threshold = 0.40; // 临时降低便于验证

    info!("🔧 Initializing CacheDB at {}", db_path);
    let db = Arc::new(CacheDB::new(db_path)?);

    info!(
        "🔧 Initializing EmbeddingService (model: {})",
        embedding_model
    );
    let embedding = Arc::new(EmbeddingService::new(ollama_url, embedding_model, 30));

    let service = CacheService {
        db,
        embedding,
        similarity_threshold,
    };

    let addr = "[::1]:50051".parse().unwrap();
    info!("🦀 semantic-cache server listening on {}", addr);

    Server::builder()
        .add_service(SemanticCacheServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
