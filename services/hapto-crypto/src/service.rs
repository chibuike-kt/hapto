use tonic::{Request, Response, Status};

use crate::generated::hapto::v1::{
    crypto_service_server::CryptoService,
    GenerateNonceRequest, GenerateNonceResponse,
    ValidatePublicKeyRequest, ValidatePublicKeyResponse,
    VerifySignatureRequest, VerifySignatureResponse,
    SignatureAlgorithm,
};
use crate::{keys, nonce, verify};

#[derive(Default)]
pub struct HaptoCryptoService;

#[tonic::async_trait]
impl CryptoService for HaptoCryptoService {
    async fn verify_signature(
        &self,
        request: Request<VerifySignatureRequest>,
    ) -> Result<Response<VerifySignatureResponse>, Status> {
        let req = request.into_inner();

        if req.algorithm() != SignatureAlgorithm::Ed25519 {
            return Ok(Response::new(VerifySignatureResponse {
                valid: false,
                reason: "unsupported algorithm".into(),
            }));
        }

        match verify::verify_ed25519(&req.public_key, &req.message, &req.signature) {
            Ok(valid) => Ok(Response::new(VerifySignatureResponse {
                valid,
                reason: if valid { String::new() } else { "signature verification failed".into() },
            })),
            Err(e) => Ok(Response::new(VerifySignatureResponse { valid: false, reason: e.0 })),
        }
    }

    async fn generate_nonce(
        &self,
        request: Request<GenerateNonceRequest>,
    ) -> Result<Response<GenerateNonceResponse>, Status> {
        let req = request.into_inner();
        let size = if req.size_bytes == 0 { 32 } else { req.size_bytes as usize };

        if size > 256 {
            return Err(Status::invalid_argument("size_bytes must not exceed 256"));
        }

        Ok(Response::new(GenerateNonceResponse { nonce: nonce::generate(size) }))
    }

    async fn validate_public_key(
        &self,
        request: Request<ValidatePublicKeyRequest>,
    ) -> Result<Response<ValidatePublicKeyResponse>, Status> {
        let req = request.into_inner();

        if req.algorithm() != SignatureAlgorithm::Ed25519 {
            return Ok(Response::new(ValidatePublicKeyResponse {
                valid: false,
                reason: "unsupported algorithm".into(),
            }));
        }

        match keys::validate_ed25519(&req.public_key) {
            Ok(()) => Ok(Response::new(ValidatePublicKeyResponse { valid: true, reason: String::new() })),
            Err(reason) => Ok(Response::new(ValidatePublicKeyResponse { valid: false, reason })),
        }
    }
}
