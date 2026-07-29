use banderwagon::{CanonicalSerialize, Element, Fr};
use ipa_multipoint::crs::CRS;
use sha2::{Digest, Sha256};

fn encode_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(DIGITS[(byte >> 4) as usize] as char);
        encoded.push(DIGITS[(byte & 0x0f) as usize] as char);
    }
    encoded
}

fn print_encodings() {
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

fn print_generators() {
    let crs = CRS::new(256, b"eth_verkle_oct_2021");
    let mut digest = Sha256::new();
    for generator in crs.G {
        digest.update(generator.to_bytes());
    }

    println!("width\tseed\tcommitments_sha256");
    println!(
        "256\teth_verkle_oct_2021\t{}",
        encode_hex(&digest.finalize())
    );
}

fn main() {
    match std::env::args().nth(1).as_deref() {
        Some("encodings") => print_encodings(),
        Some("generators") => print_generators(),
        _ => panic!("usage: verkle-tree-rust-encoding-vectors <encodings|generators>"),
    }
}
