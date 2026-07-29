use banderwagon::{CanonicalSerialize, Element, Fr};

fn encode_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(DIGITS[(byte >> 4) as usize] as char);
        encoded.push(DIGITS[(byte & 0x0f) as usize] as char);
    }
    encoded
}

fn main() {
    println!("scalar_u64\tscalar_le\tcommitment_be");
    for value in [1_u64, 2, 3, 255, 65_535] {
        let scalar = Fr::from(value);
        let mut scalar_bytes = [0_u8; 32];
        scalar
            .serialize_compressed(&mut scalar_bytes[..])
            .expect("scalar serialization");
        let commitment = Element::prime_subgroup_generator() * scalar;
        println!(
            "{value}\t{}\t{}",
            encode_hex(&scalar_bytes),
            encode_hex(&commitment.to_bytes()),
        );
    }
}
