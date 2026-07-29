use banderwagon::{CanonicalSerialize, Element, Fr, PrimeField};
use ipa_multipoint::{
    committer::DefaultCommitter,
    crs::CRS,
    lagrange_basis::{LagrangeBasis, PrecomputedWeights},
    multiproof::{MultiPoint, ProverQuery},
    transcript::Transcript,
};
use sha2::{Digest, Sha256};
use verkle_trie::{
    constants::new_crs,
    database::memory_db::MemoryDb,
    proof::{
        golang_proof_format::{bytes32_to_element, hex_to_bytes32, VerkleProofGo},
        stateless_updater::verify_and_update,
    },
    Trie, TrieTrait, VerkleConfig,
};

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

fn scalar_bytes(value: Fr) -> [u8; 32] {
    let mut encoded = [0_u8; 32];
    value
        .serialize_compressed(&mut encoded[..])
        .expect("scalar serialization");
    encoded
}

fn print_leaf_vector_row(case: &str, suffix: u8, value: Option<[u8; 32]>) {
    let half = if suffix < 128 { "C1" } else { "C2" };
    let low_index = 2 * (suffix % 128);
    let high_index = low_index + 1;
    let encoded_value = value
        .map(|bytes| encode_hex(&bytes))
        .unwrap_or_else(|| "-".to_owned());
    let (low, high) = match value {
        Some(value) => {
            let mut low_bytes = [0_u8; 17];
            low_bytes[..16].copy_from_slice(&value[..16]);
            low_bytes[16] = 1;
            (
                Fr::from_le_bytes_mod_order(&low_bytes),
                Fr::from_le_bytes_mod_order(&value[16..]),
            )
        }
        None => (Fr::from(0_u64), Fr::from(0_u64)),
    };

    println!(
        "{case}\t{suffix}\t{}\t{encoded_value}\t{half}\t{low_index}\t{high_index}\t{}\t{}",
        value.is_some(),
        encode_hex(&scalar_bytes(low)),
        encode_hex(&scalar_bytes(high)),
    );
}

fn print_leaf_vectors() {
    let mut incrementing = [0_u8; 32];
    for (index, byte) in incrementing.iter_mut().enumerate() {
        *byte = index as u8;
    }
    let mut patterned = [0_u8; 32];
    for (index, byte) in patterned.iter_mut().enumerate() {
        *byte = 0x80_u8.wrapping_add(index as u8);
    }

    println!(
        "case\tsuffix\tpresent\tvalue\thalf\tlow_index\thigh_index\tlow_scalar_le\thigh_scalar_le"
    );
    print_leaf_vector_row("present-zero-c1", 0, Some([0_u8; 32]));
    print_leaf_vector_row("present-incrementing-c1", 127, Some(incrementing));
    print_leaf_vector_row("present-ones-c2", 128, Some([0xff_u8; 32]));
    print_leaf_vector_row("present-patterned-c2", 255, Some(patterned));
    print_leaf_vector_row("absent-c1", 42, None);
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

fn tree_key(first: u8, suffix: u8) -> [u8; 32] {
    let mut key = [0_u8; 32];
    key[0] = first;
    key[31] = suffix;
    key
}

fn tree_value(seed: u8) -> [u8; 32] {
    let mut value = [0_u8; 32];
    for (index, byte) in value.iter_mut().enumerate() {
        *byte = seed.wrapping_add(index as u8);
    }
    value
}

fn print_tree_proof() {
    let entries = vec![
        (tree_key(0x00, 0x00), tree_value(0x11)),
        (tree_key(0x00, 0x01), tree_value(0x22)),
        (tree_key(0x01, 0xff), tree_value(0x33)),
        (tree_key(0x01, 0x7f), tree_value(0x44)),
    ];
    let mut trie = Trie::new(VerkleConfig::new(MemoryDb::new()));
    trie.insert(entries.into_iter());

    let keys = vec![
        tree_key(0x00, 0x00),
        tree_key(0x00, 0x02),
        tree_key(0x01, 0xff),
        tree_key(0x02, 0x00),
    ];
    let values = keys.iter().map(|key| trie.get(*key)).collect();
    let root = trie.root_commitment();
    let proof = trie
        .create_verkle_proof(keys.iter().copied())
        .expect("tree proof creation failed");
    let (verified, _) = proof.clone().check(keys, values, root);
    assert!(verified, "Rust verifier rejected generated tree proof");
    let mut proof_bytes = proof
        .proof
        .to_bytes()
        .expect("tree multiproof serialization failed");
    let scalar_offset = proof_bytes.len() - 32;
    proof_bytes[scalar_offset..].reverse();

    println!("root_commitment\tmultiproof");
    println!(
        "{}\t{}",
        encode_hex(&root.to_bytes()),
        encode_hex(&proof_bytes),
    );
}

fn verify_go_witness(path: &str, root_hex: &str) {
    let witness = std::fs::read_to_string(path).expect("read Go execution witness");
    let proof = VerkleProofGo::from_json_str(&witness);
    let (proof, keys_values) = proof
        .from_verkle_proof_go_to_verkle_proof()
        .expect("decode Go execution witness");
    let root = bytes32_to_element(hex_to_bytes32(root_hex)).expect("decode Go root");
    let (verified, _) = proof.check(keys_values.keys, keys_values.current_values, root);
    assert!(verified, "Rust verifier rejected Go execution witness");
    println!("verified");
}

fn update_go_witness(path: &str, root_hex: &str) {
    let witness = std::fs::read_to_string(path).expect("read Go update witness");
    let proof = VerkleProofGo::from_json_str(&witness);
    let (proof, keys_values) = proof
        .from_verkle_proof_go_to_verkle_proof()
        .expect("decode Go update witness");
    let root = bytes32_to_element(hex_to_bytes32(root_hex)).expect("decode Go root");
    let crs = new_crs();
    let post_root = verify_and_update(
        proof,
        root,
        keys_values.keys,
        keys_values.current_values,
        keys_values.new_values,
        DefaultCommitter::new(&crs.G),
    )
    .expect("verify and apply Go update witness");
    println!("{}", encode_hex(&post_root.to_bytes()));
}

fn main() {
    let mut arguments = std::env::args().skip(1);
    match arguments.next().as_deref() {
        Some("encodings") => print_encodings(),
        Some("leaf-vectors") => print_leaf_vectors(),
        Some("generators") => print_generators(),
        Some("multiproof") => print_multiproof(),
        Some("tree-proof") => print_tree_proof(),
        Some("verify-go-witness") => verify_go_witness(
            &arguments.next().expect("missing Go execution witness path"),
            &arguments.next().expect("missing Go root"),
        ),
        Some("update-go-witness") => update_go_witness(
            &arguments.next().expect("missing Go update witness path"),
            &arguments.next().expect("missing Go root"),
        ),
        _ => panic!(
            "usage: verkle-tree-rust-encoding-vectors \
             <encodings|leaf-vectors|generators|multiproof|tree-proof|verify-go-witness|\
             update-go-witness>"
        ),
    }
}
