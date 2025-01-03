use clap::Parser;
use flate2::{write::GzEncoder, Compression};
use std::fs::File;
use std::path::PathBuf;
use tar::Builder;

use cloudflare_ssh::{bootstrap, CloudflareSsh};

use std::io::Write;

/// Deploy an app
#[derive(Parser, Debug)]
#[command(version, about, long_about = None)]
struct Args {
    /// directory of Rust app to deploy
    #[arg(short, long)]
    project_path: PathBuf,

    /// Name of app to deploy
    #[arg(short, long)]
    app_name: String,

    /// Username on remote
    #[arg(short, long)]
    remote_username: String,
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

    println!("bootstrapping deployment configurations");
    bootstrap(&args.app_name, &args.remote_username)
        .expect("Should have been able to bootstrap deployment config");

    let cloudflare_ssh_client =
        CloudflareSsh::new().expect("Unable to create cloudflare ssh client");

    println!("creating /opt/{} dir", args.app_name);
    let result = cloudflare_ssh_client
        .exec(&format!("sudo mkdir -p /opt/{}", args.app_name))
        .expect(&format!(
            "Should have been able to make /opt/{} dir",
            args.app_name
        ));
    println!("{}", result);
    println!(
        "setting ownership of /opt/{} to {}",
        args.app_name, args.remote_username
    );
    let result = cloudflare_ssh_client
        .exec(&format!(
            "sudo chown -R {}:{} /opt/{}",
            args.remote_username, args.remote_username, args.app_name
        ))
        .expect(&format!(
            "Should have been able to set ownership of /opt/{} to {}",
            args.app_name, args.remote_username
        ));
    println!("{}", result);
    let bytes_sent = cloudflare_ssh_client
        .scp(
            "/Users/deepwater/archive.tar.gz",
            &format!("/opt/{}/archive.tar.gz", args.app_name),
        )
        .expect("Unable to scp tarball to remote");
    println!("sent {} bytes", bytes_sent);

    println!("extracting tarball");
    cloudflare_ssh_client
        .exec(&format!(
            "tar -xvf /opt/{}/archive.tar.gz -C /opt/{}",
            args.app_name, args.app_name
        ))
        .expect("Unable to extract tarball");

    println!("checking cargo installation");
    cloudflare_ssh_client
        .exec("which cargo && cargo --version && pwd")
        .expect("Unable to check cargo installation");

    println!("running cargo build");
    cloudflare_ssh_client
        .exec(&format!(
            "source $HOME/.cargo/env && cd /opt/{} && cargo build --release",
            args.app_name
        ))
        .expect("Unable to cargo build");

    println!("getting free port number from rproxy");
    let params = [("app", &args.app_name)];
    let url = reqwest::Url::parse_with_params("http://localhost:3002/api/port", &params)
        .expect("Should have been able to parse request url with params");
    let res = reqwest::blocking::get(url).expect("Should have been able to request port number");
    let port = res
        .text()
        .expect("Should have been able to convert result to port number");
    println!("got free port {}", port);

    let service_file_contents = format!(
        "[Unit]
         Description=Hello world server
         After=network.target

         [Service]
         ExecStart=/opt/axum-hello-world/target/release/axum-hello-world --port {port}
         Type=simple
         Restart=always

         [Install]
         WantedBy=default.target
         RequiredBy=network.target"
    );

    println!("writing service file");
    cloudflare_ssh_client
        .exec(&format!(
            "echo \"{}\" | sudo tee /etc/systemd/system/axum-hello-world.service",
            service_file_contents
        ))
        .expect("Should have been able to write systemd service file");

    println!("Issuing systemd reload command");
    cloudflare_ssh_client
        .exec("sudo systemctl daemon-reload")
        .expect("Should have been able to reload systemd");

    println!("Enabling {} service", args.app_name);
    cloudflare_ssh_client
        .exec(&format!("sudo systemctl enable {}.service", args.app_name))
        .expect("Should have been able to enable service");

    println!("Starting {} service", args.app_name);
    cloudflare_ssh_client
        .exec(&format!("sudo systemctl start {}.service", args.app_name))
        .expect("Should have been able to enable service");

    println!("finished");
}
