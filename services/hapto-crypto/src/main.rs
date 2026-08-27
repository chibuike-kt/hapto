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

use std::env;
use std::fs;

use tonic::transport::{Certificate, Identity, Server, ServerTlsConfig};

use generated::hapto::v1::crypto_service_server::CryptoServiceServer;
use service::HaptoCryptoService;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = env::var("HAPTO_CRYPTO_LISTEN_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:50051".to_string())
        .parse()?;
    let service = HaptoCryptoService;
    let tls_config = load_tls_config()?;

    println!("hapto-crypto listening on {addr} (mTLS required, no plaintext fallback)");

    Server::builder()
        .tls_config(tls_config)?
        .add_service(CryptoServiceServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}

/// Loads hapto-crypto's mTLS configuration: its own server certificate/key,
/// and the CA used to verify client certificates. Setting `client_ca_root`
/// without `client_auth_optional(true)` makes tonic require a client
/// certificate signed by that CA on every connection — there is no
/// plaintext or server-auth-only fallback.
fn load_tls_config() -> Result<ServerTlsConfig, Box<dyn std::error::Error>> {
    let cert_path =
        env::var("HAPTO_CRYPTO_TLS_CERT").unwrap_or_else(|_| "../../certs/hapto-crypto.crt".to_string());
    let key_path =
        env::var("HAPTO_CRYPTO_TLS_KEY").unwrap_or_else(|_| "../../certs/hapto-crypto.key".to_string());
    let ca_path = env::var("HAPTO_CRYPTO_TLS_CA").unwrap_or_else(|_| "../../certs/ca.crt".to_string());

    let cert = fs::read(&cert_path).map_err(|e| format!("read {cert_path}: {e}"))?;
    let key = fs::read(&key_path).map_err(|e| format!("read {key_path}: {e}"))?;
    let ca = fs::read(&ca_path).map_err(|e| format!("read {ca_path}: {e}"))?;

    Ok(ServerTlsConfig::new()
        .identity(Identity::from_pem(cert, key))
        .client_ca_root(Certificate::from_pem(ca)))
}
