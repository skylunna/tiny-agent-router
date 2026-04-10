fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true) // 生成服务端 Trait（你必须实现它）
        .build_client(false) // 客户端在 Go 侧，这里不需要
        .out_dir("src/proto") // 🔑 关键：直接输出到源码目录，方便查看与 Git 追踪
        .compile_protos(&["../proto/cache.proto"], &["../proto"])?;

    // 监听文件变化，只有 .proto 或 build.rs 改动时才重新生成
    println!("cargo:rerun-if-changed=../proto/cache.proto");
    println!("cargo:rerun-if-changed=build.rs");
    Ok(())
}
