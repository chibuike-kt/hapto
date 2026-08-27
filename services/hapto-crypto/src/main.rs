mod keys;
mod nonce;
mod service;
mod verify;

pub mod generated {
    pub mod hapto {
        pub mod v1 {
            tonic::include_proto!("hapto.v1");
        }
    }
}

use tonic::transport::Server;

use generated::hapto::v1::crypto_service_server::CryptoServiceServer;
use service::HaptoCryptoService;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = "0.0.0.0:50051".parse()?;
    let service = HaptoCryptoService;

    println!("hapto-crypto listening on {addr}");

    Server::builder()
        .add_service(CryptoServiceServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
