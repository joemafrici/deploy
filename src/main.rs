use clap::Parser;
use flate2::{write::GzEncoder, Compression};
use std::fs::File;
use std::path::PathBuf;
use tar::Builder;

use cloudflare_ssh::{bootstrap, CloudflareSsh};

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
    let encoder = archive.into_inner().expect("Unable to get encoder back");
    encoder.finish().expect("Unable to finish compression");
    println!("Tarball created");

    // println!("bootstrapping deployment configurations");
    // bootstrap("axum-hello-world").expect("Should have been able to bootstrap deployment config");

    let cloudflare_ssh_client =
        CloudflareSsh::new().expect("Unable to create cloudflare ssh client");

    println!("creating /opt/axum-hello-world dir");
    let result = cloudflare_ssh_client
        .exec("sudo mkdir -p /opt/axum-hello-world")
        .expect("Should have been able to make /opt/axum-hello-world dir");
    println!("{}", result);
    println!("setting ownership of /opt/axum-hello-world to deepwater");
    let result = cloudflare_ssh_client
        .exec("sudo chown -R deepwater:deepwater /opt/axum-hello-world")
        .expect("Should have been able to set ownership of /opt/axum-hello-world to deepwater");
    println!("{}", result);
    let bytes_sent = cloudflare_ssh_client
        .scp(
            "/Users/deepwater/archive.tar.gz",
            "/opt/axum-hello-world/archive.tar.gz",
        )
        .expect("Unable to scp tarball to remote");
    println!("sent {} bytes", bytes_sent);

    println!("extracting tarball");
    let result = cloudflare_ssh_client
        .exec("tar -xvf /opt/axum-hello-world/archive.tar.gz -C /opt/axum-hello-world")
        .expect("Unable to extract tarball");
    println!("{}", result);

    println!("checking cargo installation");
    cloudflare_ssh_client
        .exec("which cargo && cargo --version && pwd")
        .expect("Unable to check cargo installation");

    println!("running cargo build");
    cloudflare_ssh_client
        .exec("source $HOME/.cargo/env && cd /opt/axum-hello-world && cargo build --release")
        .expect("Unable to cargo build");
}
