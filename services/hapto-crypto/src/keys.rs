use ed25519_dalek::VerifyingKey;

pub fn validate_ed25519(public_key: &[u8]) -> Result<(), String> {
    let key_bytes: [u8; 32] = public_key
        .try_into()
        .map_err(|_| "public key must be 32 bytes".to_string())?;
    VerifyingKey::from_bytes(&key_bytes)
        .map(|_| ())
        .map_err(|e| format!("invalid public key: {e}"))
}
