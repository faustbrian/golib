use banderwagon::{CanonicalSerialize, Element, Fr};
use ipa_multipoint::{
    crs::CRS,
    lagrange_basis::{LagrangeBasis, PrecomputedWeights},
    multiproof::{MultiPoint, ProverQuery},
    transcript::Transcript,
};
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

fn print_multiproof() {
    let crs = CRS::new(256, b"eth_verkle_oct_2021");
    let precomputed_weights = PrecomputedWeights::new(256);
    let points = [3_usize, 3, 200];
    let polynomials = [
        (0_u128..256).map(|index| Fr::from(index + 1)).collect(),
        (0_u128..256)
            .map(|index| Fr::from((index + 1) * (index + 1)))
            .collect(),
        (0_u128..256).map(|index| Fr::from(3 * index + 7)).collect(),
    ];
    let queries = polynomials
        .into_iter()
        .zip(points)
        .map(|(values, point)| {
            let poly = LagrangeBasis::new(values);
            ProverQuery {
                commitment: crs.commit_lagrange_poly(&poly),
                result: poly.evaluate_in_domain(point),
                poly,
                point,
            }
        })
        .collect();
    let mut transcript = Transcript::new(b"verkle");
    let proof = MultiPoint::open(crs, &precomputed_weights, &mut transcript, queries);

    println!("corpus\ttranscript\tproof");
    println!(
        "three-openings-v1\tverkle\t{}",
        encode_hex(&proof.to_bytes().expect("multiproof serialization failed"))
    );
}

fn main() {
    match std::env::args().nth(1).as_deref() {
        Some("encodings") => print_encodings(),
        Some("generators") => print_generators(),
        Some("multiproof") => print_multiproof(),
        _ => panic!("usage: verkle-tree-rust-encoding-vectors <encodings|generators|multiproof>"),
    }
}
