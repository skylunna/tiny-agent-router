use crate::embedding::EmbeddingService;
use crate::proto::cache::semantic_cache_server::SemanticCache;
use crate::proto::cache::{CacheRequest, CacheResponse, Empty, HealthResponse};
use crate::storage::CacheDB;
use sha2::{Digest, Sha256};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};
use tonic::{Request, Response, Status};
use tracing::{info, warn};
use uuid::Uuid;

#[derive(Clone)]
pub struct CacheService {
    pub db: Arc<CacheDB>,
    pub embedding: Arc<EmbeddingService>,
    pub similarity_threshold: f32,
}

#[tonic::async_trait]
impl SemanticCache for CacheService {
    async fn get(&self, request: Request<CacheRequest>) -> Result<Response<CacheResponse>, Status> {
        let req = request.into_inner();
        info!(
            "🔍 DEBUG GET: request_id={}, model={}, prompt_text='{}'",
            req.request_id, req.model, req.prompt_text
        );

        let prompt = if !req.prompt_text.is_empty() {
            &req.prompt_text
        } else {
            warn!("⚠️ prompt_text is empty, falling back to request_id");
            &req.request_id
        };

        let embedding = match self.embedding.generate(prompt).await {
            Ok(emb) => {
                info!("🔍 DEBUG: Generated embedding, dims={}", emb.len());
                emb
            }
            Err(e) => {
                warn!("❌ Embedding generation failed: {}", e);
                return Ok(Response::new(CacheResponse {
                    hit: false,
                    response_body: None,
                    similarity: None,
                    reason: "embedding_error".to_string(),
                }));
            }
        };

        info!(
            "🔍 DEBUG: Querying DB for model='{}', threshold={}",
            req.model, self.similarity_threshold
        );

        match self
            .db
            .find_similar(&embedding, &req.model, self.similarity_threshold)
        {
            Ok(Some(entry)) => {
                let sim = crate::storage::cosine_similarity(&embedding, &entry.embedding);
                info!("✅ Cache HIT: similarity={:.6}, entry_id={}", sim, entry.id);

                Ok(Response::new(CacheResponse {
                    hit: true,
                    response_body: Some(entry.response_body),
                    similarity: Some(sim),
                    reason: "hit".to_string(),
                }))
            }
            Ok(None) => {
                if let Ok(count) = self.db.count_entries(&req.model) {
                    warn!(
                        "❌ Cache MISS: model='{}' has {} entries in DB",
                        req.model, count
                    );
                } else {
                    warn!("❌ Cache MISS: model='{}'", req.model);
                }

                Ok(Response::new(CacheResponse {
                    hit: false,
                    response_body: None,
                    similarity: None,
                    reason: "miss".to_string(),
                }))
            }
            Err(e) => {
                warn!("❌ Cache query failed: {}", e);
                Ok(Response::new(CacheResponse {
                    hit: false,
                    response_body: None,
                    similarity: None,
                    reason: "storage_error".to_string(),
                }))
            }
        }
    }

    async fn put(&self, request: Request<CacheRequest>) -> Result<Response<Empty>, Status> {
        let req = request.into_inner();
        info!("📥 PUT request: id={}, model={}", req.request_id, req.model);

        let prompt = if !req.prompt_text.is_empty() {
            req.prompt_text
        } else {
            req.request_id.clone()
        };

        let embedding = match self.embedding.generate(&prompt).await {
            Ok(emb) => emb,
            Err(e) => {
                warn!("❌ PUT Embedding failed: {}", e);
                return Ok(Response::new(Empty {}));
            }
        };

        let response_body = req.response_body.clone().unwrap_or_default();
        let input_tokens = req.input_tokens.unwrap_or(0);
        let output_tokens = req.output_tokens.unwrap_or(0);
        let created_at = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        let hash = {
            let mut hasher = Sha256::new();
            hasher.update(format!(
                "{}|{}|{}",
                req.request_id, req.model, req.system_prompt_hash
            ));
            hex::encode(hasher.finalize())
        };

        let entry = crate::storage::CacheEntry {
            id: Uuid::new_v4().to_string(),
            request_hash: hash,
            embedding,
            response_body,
            input_tokens,
            output_tokens,
            created_at,
            model: req.model,
        };

        let entry_id = entry.id.clone();
        let entry_model = entry.model.clone();

        if let Err(e) = self.db.insert(entry) {
            warn!("❌ Failed to insert cache entry: {}", e);
        } else {
            info!(
                "✅ Cache entry COMMITTED to DB: id={}, model={}",
                entry_id, entry_model
            );
        }

        Ok(Response::new(Empty {}))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        Ok(Response::new(HealthResponse { serving: true }))
    }
}
