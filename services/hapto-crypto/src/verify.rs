use ed25519_dalek::{Signature, VerifyingKey};

pub struct VerifyError(pub String);

pub fn verify_ed25519(public_key: &[u8], message: &[u8], signature: &[u8]) -> Result<bool, VerifyError> {
    let key_bytes: [u8; 32] = public_key
        .try_into()
        .map_err(|_| VerifyError("public key must be 32 bytes".into()))?;
    let verifying_key = VerifyingKey::from_bytes(&key_bytes)
        .map_err(|e| VerifyError(format!("invalid public key: {e}")))?;

    let sig_bytes: [u8; 64] = signature
        .try_into()
        .map_err(|_| VerifyError("signature must be 64 bytes".into()))?;
    let signature = Signature::from_bytes(&sig_bytes);

    Ok(verifying_key.verify_strict(message, &signature).is_ok())
}
