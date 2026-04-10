// cache-service/src/storage.rs
use rusqlite::{Connection, params};
use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use tracing::info;

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct CacheEntry {
    pub id: String,
    pub request_hash: String,
    pub embedding: Vec<f32>,
    pub response_body: String,
    pub input_tokens: i32,
    pub output_tokens: i32,
    pub created_at: i64,
    pub model: String,
}

pub struct CacheDB {
    conn: Mutex<Connection>, // ✅ 用 Mutex 包装，使其线程安全 (Sync)
}

impl CacheDB {
    pub fn new(path: &str) -> Result<Self, rusqlite::Error> {
        let conn = Connection::open(path)?;

        conn.execute(
            "CREATE TABLE IF NOT EXISTS cache_entries (
                id TEXT PRIMARY KEY,
                request_hash TEXT NOT NULL,
                embedding BLOB NOT NULL,
                response_body TEXT NOT NULL,
                input_tokens INTEGER NOT NULL,
                output_tokens INTEGER NOT NULL,
                created_at INTEGER NOT NULL,
                model TEXT NOT NULL
            )",
            [],
        )?;

        Ok(Self {
            conn: Mutex::new(conn),
        })
    }

    pub fn insert(&self, entry: CacheEntry) -> Result<(), rusqlite::Error> {
        // ✅ 获取锁（自动在作用域结束时释放）
        let conn = self.conn.lock().unwrap();
        let embedding_bytes = bytemuck::cast_slice::<f32, u8>(&entry.embedding);

        conn.execute(
            "INSERT OR REPLACE INTO cache_entries 
             (id, request_hash, embedding, response_body, input_tokens, output_tokens, created_at, model)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
            params![
                entry.id, entry.request_hash, embedding_bytes,
                entry.response_body, entry.input_tokens, entry.output_tokens,
                entry.created_at, entry.model
            ],
        )?;
        Ok(())
    }

    pub fn find_similar(
        &self,
        target_embedding: &[f32],
        model: &str,
        threshold: f32,
    ) -> Result<Option<CacheEntry>, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();

        let mut stmt = conn.prepare(
        "SELECT id, request_hash, embedding, response_body, input_tokens, output_tokens, created_at, model
         FROM cache_entries 
         WHERE model = ?1 
         ORDER BY created_at DESC 
         LIMIT 10"
    )?;

        let rows = stmt.query_map([model], |row| {
            let embedding_bytes: Vec<u8> = row.get(2)?;
            let embedding = bytemuck::cast_slice::<u8, f32>(&embedding_bytes).to_vec();

            Ok(CacheEntry {
                id: row.get(0)?,
                request_hash: row.get(1)?,
                embedding,
                response_body: row.get(3)?,
                input_tokens: row.get(4)?,
                output_tokens: row.get(5)?,
                created_at: row.get(6)?,
                model: row.get(7)?,
            })
        })?;

        let mut best_sim = 0.0;
        let mut best_id = String::new();

        for entry in rows.flatten() {
            let sim = cosine_similarity(target_embedding, &entry.embedding);

            // 🔥 关键调试：打印每条记录的相似度
            tracing::info!(
                "🔍 DEBUG: Comparing entry={} | similarity={:.6}",
                entry.id,
                sim
            );

            if sim > best_sim {
                best_sim = sim;
                best_id = entry.id.clone();
            }

            if sim >= threshold {
                tracing::info!("✅ Cache HIT: similarity={:.6}, entry_id={}", sim, entry.id);
                return Ok(Some(entry));
            }
        }

        // 🔥 如果没命中，打印最佳匹配
        if !best_id.is_empty() {
            tracing::warn!(
                "❌ Cache MISS: best_similarity={:.6} < threshold={} | best_entry={}",
                best_sim,
                threshold,
                best_id
            );
        } else {
            tracing::warn!("❌ Cache MISS: no entries found for model={}", model);
        }

        Ok(None)
    }

    pub fn count_entries(&self, model: &str) -> Result<i64, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.query_row(
            "SELECT COUNT(*) FROM cache_entries WHERE model = ?1",
            [model],
            |row| row.get(0),
        )
    }
}

// 余弦相似度计算（纯函数，无状态，天然线程安全）
pub fn cosine_similarity(a: &[f32], b: &[f32]) -> f32 {
    if a.len() != b.len() || a.is_empty() {
        return 0.0;
    }
    let dot: f32 = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
    let norm_a: f32 = a.iter().map(|x| x * x).sum::<f32>().sqrt();
    let norm_b: f32 = b.iter().map(|x| x * x).sum::<f32>().sqrt();
    if norm_a < 1e-8 || norm_b < 1e-8 {
        return 0.0;
    }
    dot / (norm_a * norm_b)
}
