use tonic::{Request, Response, Status, transport::Server};

pub mod proto {
    tonic::include_proto!("cache");
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

        Ok(Response::new(Empty {}))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        Ok(Response::new(HealthResponse { serving: true }))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_max_level(tracing::Level::INFO)
        .with_target(false)
        .init();

    let addr = "[::1]:50051".parse().unwrap();
    let service = CacheService;

    tracing::info!("🦀 semantic-cache server listening on {}", addr);

    Server::builder()
        .add_service(SemanticCacheServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
