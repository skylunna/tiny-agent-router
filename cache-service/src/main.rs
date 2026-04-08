use tonic::{transport::Server, Request, Response, Status};
use tracing_subscriber;

// 自动生成的 proto 模块
pub mod proto {
    tonic::include_proto!("cache"); // 根据 package name
}

use proto::semantic_cache_server::{SemanticCache, SemanticCacheServer};
use proto::{CacheRequest, CacheResponse, Empty, HealthResponse};

#[derive(Default)]
struct CacheService;

#[tonic::async_trait]
impl SemanticCache for CacheService {
    async fn get(&self, request: Request<CacheRequest>) -> Result<Response<CacheResponse>, Status> {
        let req = request.into_inner();
        tracing::info!("Cache GET request: id={}", req.request_id);

        // Step 4.1 MVP: 始终返回未命中，但记录日志
        // Step 4.2 将实现：embedding 相似度 + SQLite 查询
        Ok(Response::new(CacheResponse {
            hit: false,
            response_body: None,
            similarity: None,
            reason: "not_implemented".to_string(),
        }))
    }

    async fn put(&self, request: Request<CacheRequest>) -> Result<Response<Empty>, Status> {
        let req = request.into_inner();
        tracing::info!("Cache PUT request: id={}", req.request_id);
        // MVP: 直接返回成功，异步逻辑后续实现
        Ok(Response::new(Empty {}))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        Ok(Response::new(HealthResponse { serving: true }))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // 初始化日志
    tracing_subscriber::fmt::init();

    let addr = "[::1]:50051".parse().unwrap();
    let service = CacheService::default();

    tracing::info!("🦀 semantic-cache server listening on {}", addr);

    Server::builder()
        .add_service(SemanticCacheServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
