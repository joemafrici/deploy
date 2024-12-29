use flate2::{write::GzEncoder, Compression};
use std::fs::File;
use tar::Builder;
fn main() {
    let file_ref = File::create("/Users/deepwater/code/archive.tar.gz")
        .expect("Unable to create archive file");
    let encoder = GzEncoder::new(file_ref, Compression::default());
    let mut archive = Builder::new(encoder);
    archive
        .append_dir_all("src", "/Users/deepwater/code/rust/axum-hello-world/src")
        .expect("Error adding file to archive");
    archive
        .append_file(
            "Cargo.lock",
            &mut File::open("/Users/deepwater/code/rust/axum-hello-world/Cargo.lock")
                .expect("Unable to open Cargo.lock file"),
        )
        .expect("Unable to add Cargo.lock to archive");
    archive
        .append_file(
            "Cargo.toml",
            &mut File::open("/Users/deepwater/code/rust/axum-hello-world/Cargo.toml")
                .expect("Unable to open Cargo.tomlfile"),
        )
        .expect("Unable to add Cargo.lock to archive");
    archive.finish().expect("Failed to create tarball");
    println!("Tarball created");
}
