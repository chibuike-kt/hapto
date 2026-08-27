use rand::RngCore;
use rand::rngs::OsRng;

pub fn generate(size_bytes: usize) -> Vec<u8> {
    let mut buf = vec![0u8; size_bytes];
    OsRng.fill_bytes(&mut buf);
    buf
}
