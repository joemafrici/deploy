use clap::Parser;
use flate2::{write::GzEncoder, Compression};
use std::fs::File;
use std::path::PathBuf;
use tar::Builder;

/// Deploy an app
#[derive(Parser, Debug)]
#[command(version, about, long_about = None)]
struct Args {
    /// directory of Rust app to deploy
    #[arg(short, long)]
    project_path: PathBuf,
}
fn main() {
    let args = Args::parse();
    let mut project_dir = args.project_path;
    let file_ref =
        File::create("/Users/deepwater/archive.tar.gz").expect("Unable to create archive file");
    let encoder = GzEncoder::new(file_ref, Compression::default());
    let mut archive = Builder::new(encoder);
    project_dir.push("src");
    archive
        .append_dir_all("src", &project_dir)
        .expect("Error adding file to archive");
    project_dir.pop();
    project_dir.push("Cargo.lock");
    archive
        .append_file(
            "Cargo.lock",
            &mut File::open(&project_dir).expect("Unable to open Cargo.lock file"),
        )
        .expect("Unable to add Cargo.lock to archive");
    project_dir.pop();
    project_dir.push("Cargo.toml");
    archive
        .append_file(
            "Cargo.toml",
            &mut File::open(&project_dir).expect("Unable to open Cargo.tomlfile"),
        )
        .expect("Unable to add Cargo.lock to archive");
    archive.finish().expect("Failed to create tarball");
    println!("Tarball created");
}
