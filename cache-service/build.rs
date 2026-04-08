fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)  // 生成服务端代码
        .build_client(false) // 客户端由 Go 侧生成
        .compile(&["../proto/cache.proto"], &["../proto"])?;
    Ok(())
}