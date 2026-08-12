use banderwagon::{CanonicalSerialize, Element, Fr, PrimeField, Zero};
use ipa_multipoint::{
    committer::DefaultCommitter,
    crs::CRS,
    lagrange_basis::{LagrangeBasis, PrecomputedWeights},
    multiproof::{MultiPoint, ProverQuery},
    transcript::Transcript,
};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
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

fn print_commitment_hashes() {
    println!("scalar_u64\tcommitment_be\tmapped_scalar_le");
    let identity = Element::zero();
    println!(
        "identity\t{}\t{}",
        encode_hex(&identity.to_bytes()),
        encode_hex(&scalar_bytes(identity.map_to_scalar_field())),
    );
    for value in [1_u64, 2, 3, 255, 65_535] {
        let commitment = Element::prime_subgroup_generator() * Fr::from(value);
        println!(
            "{value}\t{}\t{}",
            encode_hex(&commitment.to_bytes()),
            encode_hex(&scalar_bytes(commitment.map_to_scalar_field())),
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

fn print_vector_commitment_row(case: &str, values: Vec<Fr>) {
    let crs = CRS::new(256, b"eth_verkle_oct_2021");
    let non_zero = values.iter().filter(|value| !value.is_zero()).count();
    let commitment = crs.commit_lagrange_poly(&LagrangeBasis::new(values));

    println!(
        "{case}\t{non_zero}\t{}\t{}",
        encode_hex(&commitment.to_bytes()),
        encode_hex(&scalar_bytes(commitment.map_to_scalar_field())),
    );
}

fn print_vector_commitments() {
    println!("case\tnon_zero_terms\tcommitment_be\tmapped_scalar_le");
    print_vector_commitment_row("zero", vec![Fr::from(0_u64); 256]);

    let mut first = vec![Fr::from(0_u64); 256];
    first[0] = Fr::from(1_u64);
    print_vector_commitment_row("one-hot-first", first);

    let mut last = vec![Fr::from(0_u64); 256];
    last[255] = Fr::from(2_u64);
    print_vector_commitment_row("one-hot-last", last);

    let mut sparse = vec![Fr::from(0_u64); 256];
    for (index, value) in [(0, 1_u64), (1, 2), (127, 3), (128, 255), (255, 65_535)] {
        sparse[index] = Fr::from(value);
    }
    print_vector_commitment_row("sparse-boundaries", sparse);

    let dense = (1_u64..=256).map(Fr::from).collect();
    print_vector_commitment_row("dense-incrementing", dense);
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

    let mut zero_values = vec![Fr::from(0_u64); 256];
    zero_values[0] = Fr::from(1_u64);
    zero_values[2] = Fr::from(2_u64);
    let zero_poly = LagrangeBasis::new(zero_values);
    let zero_crs = CRS::new(256, b"eth_verkle_oct_2021");
    let zero_query = ProverQuery {
        commitment: zero_crs.commit_lagrange_poly(&zero_poly),
        result: zero_poly.evaluate_in_domain(1),
        poly: zero_poly,
        point: 1,
    };
    let mut zero_transcript = Transcript::new(b"verkle");
    let zero_proof = MultiPoint::open(
        zero_crs,
        &precomputed_weights,
        &mut zero_transcript,
        vec![zero_query],
    );
    println!(
        "one-zero-evaluation-v1\tverkle\t{}",
        encode_hex(
            &zero_proof
                .to_bytes()
                .expect("zero-evaluation multiproof serialization failed")
        )
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

fn print_tree_root_row(case: &str, entries: Vec<([u8; 32], [u8; 32])>) {
    let encoded_entries = if entries.is_empty() {
        "-".to_owned()
    } else {
        entries
            .iter()
            .map(|(key, value)| format!("{}:{}", encode_hex(key), encode_hex(value)))
            .collect::<Vec<_>>()
            .join(",")
    };
    let mut trie = Trie::new(VerkleConfig::new(MemoryDb::new()));
    trie.insert(entries.into_iter());
    println!(
        "{case}\t{encoded_entries}\t{}",
        encode_hex(&trie.root_commitment().to_bytes()),
    );
}

fn print_tree_roots() {
    println!("case\tentries\troot_commitment_be");
    print_tree_root_row("empty", vec![]);
    print_tree_root_row("present-zero", vec![(tree_key(0, 0), [0_u8; 32])]);
    print_tree_root_row(
        "single-patterned",
        vec![(tree_key(0x10, 0x7f), tree_value(0x11))],
    );
    print_tree_root_row(
        "same-stem-boundaries",
        vec![
            (tree_key(0x20, 0x00), tree_value(0x22)),
            (tree_key(0x20, 0xff), tree_value(0x33)),
        ],
    );
    print_tree_root_row(
        "separate-root-branches",
        vec![
            (tree_key(0x00, 0x00), tree_value(0x11)),
            (tree_key(0x00, 0x01), tree_value(0x22)),
            (tree_key(0x01, 0xff), tree_value(0x33)),
            (tree_key(0x01, 0x7f), tree_value(0x44)),
        ],
    );

    let mut deepest_left = [0_u8; 32];
    deepest_left[30] = 1;
    let mut deepest_right = [0_u8; 32];
    deepest_right[30] = 2;
    print_tree_root_row(
        "deepest-collision",
        vec![
            (deepest_left, tree_value(0x55)),
            (deepest_right, tree_value(0x66)),
        ],
    );
}

#[derive(Clone, Copy)]
enum TreeUpdate {
    Set([u8; 32], [u8; 32]),
    Delete([u8; 32]),
}

const TRANSITION_TRACE_COUNT: usize = 2_048;
const CURATED_TRANSITION_TRACE_COUNT: usize = 10;
const GENERATED_TRANSITION_SEED: u64 = 0x7665_726b_6c65_7630;

fn tree_root(entries: &[([u8; 32], [u8; 32])]) -> Element {
    let mut trie = Trie::new(VerkleConfig::new(MemoryDb::new()));
    trie.insert(entries.iter().copied());
    trie.root_commitment()
}

fn encode_transition_entries(entries: &[([u8; 32], [u8; 32])]) -> String {
    if entries.is_empty() {
        return "-".to_owned();
    }
    entries
        .iter()
        .map(|(key, value)| format!("{}:{}", encode_hex(key), encode_hex(value)))
        .collect::<Vec<_>>()
        .join(",")
}

fn encode_transition_updates(updates: &[TreeUpdate]) -> String {
    updates
        .iter()
        .map(|update| match update {
            TreeUpdate::Set(key, value) => {
                format!("set:{}:{}", encode_hex(key), encode_hex(value))
            }
            TreeUpdate::Delete(key) => format!("delete:{}", encode_hex(key)),
        })
        .collect::<Vec<_>>()
        .join(",")
}

fn print_transition_row(
    case: &str,
    pre_entries: Vec<([u8; 32], [u8; 32])>,
    updates: Vec<TreeUpdate>,
) {
    let pre_root = tree_root(&pre_entries);
    let mut post_entries = pre_entries.iter().copied().collect::<BTreeMap<_, _>>();
    for update in updates.iter().copied() {
        match update {
            TreeUpdate::Set(key, value) => {
                post_entries.insert(key, value);
            }
            TreeUpdate::Delete(key) => {
                post_entries.remove(&key);
            }
        }
    }
    let post_entries = post_entries.into_iter().collect::<Vec<_>>();
    let post_root = tree_root(&post_entries);
    println!(
        "{case}\t{}\t{}\t{}\t{}",
        encode_transition_entries(&pre_entries),
        encode_transition_updates(&updates),
        encode_hex(&pre_root.to_bytes()),
        encode_hex(&post_root.to_bytes()),
    );
}

fn next_transition_word(state: &mut u64) -> u64 {
    *state ^= *state << 13;
    *state ^= *state >> 7;
    *state ^= *state << 17;
    *state
}

fn generated_transition_value(state: &mut u64, zero: bool) -> [u8; 32] {
    if zero {
        return [0_u8; 32];
    }
    let mut value = [0_u8; 32];
    for chunk in value.chunks_exact_mut(8) {
        chunk.copy_from_slice(&next_transition_word(state).to_le_bytes());
    }
    value
}

fn generated_transition_keys(state: &mut u64) -> [[u8; 32]; 6] {
    let mut base = [0_u8; 32];
    for chunk in base[..24].chunks_exact_mut(8) {
        chunk.copy_from_slice(&next_transition_word(state).to_le_bytes());
    }
    base[24..31].copy_from_slice(&next_transition_word(state).to_le_bytes()[..7]);
    base[31] = 0x11;

    let collision_depth = (next_transition_word(state) % 31) as usize;
    let mut keys = [base; 6];
    keys[1][31] = 0xee;
    keys[2][collision_depth] ^= 0x80;
    keys[2][31] = 0x22;
    keys[3][collision_depth] ^= 0x40;
    keys[3][31] = 0x33;
    keys[4][0] ^= 0x20;
    keys[4][31] = 0x44;
    keys[5] = keys[2];
    keys[5][31] = 0xdd;
    keys
}

fn print_generated_transitions() {
    for case_index in 0..(TRANSITION_TRACE_COUNT - CURATED_TRANSITION_TRACE_COUNT) {
        let case_seed = GENERATED_TRANSITION_SEED
            ^ (case_index as u64).wrapping_mul(0x9e37_79b9_7f4a_7c15);
        let mut state = case_seed;
        let keys = generated_transition_keys(&mut state);
        let pre_count = (next_transition_word(&mut state) % 5) as usize;
        let mut pre_entries = Vec::with_capacity(pre_count);
        for (entry_index, key) in keys.iter().copied().take(pre_count).enumerate() {
            pre_entries.push((
                key,
                generated_transition_value(
                    &mut state,
                    (case_index + entry_index) % 37 == 0,
                ),
            ));
        }
        if case_index % 2 == 1 {
            pre_entries.reverse();
        }

        let update_count = 1 + (next_transition_word(&mut state) % 3) as usize;
        let update_offset = (next_transition_word(&mut state) % 4) as usize;
        let mut updates = Vec::with_capacity(update_count);
        for update_index in 0..update_count {
            let key = keys[update_offset + update_index];
            if next_transition_word(&mut state) & 1 == 0 {
                updates.push(TreeUpdate::Set(
                    key,
                    generated_transition_value(
                        &mut state,
                        (case_index + update_index) % 41 == 0,
                    ),
                ));
            } else {
                updates.push(TreeUpdate::Delete(key));
            }
        }
        if case_index % 3 == 2 {
            updates.reverse();
        }

        print_transition_row(
            &format!("generated-{case_index:04}-{case_seed:016x}"),
            pre_entries,
            updates,
        );
    }
}

fn print_transitions() {
    println!("case\tpre_entries\tupdates\tpre_root_commitment_be\tpost_root_commitment_be");

    let updated = tree_key(0x10, 0x01);
    print_transition_row(
        "update-present",
        vec![(updated, tree_value(0x11))],
        vec![TreeUpdate::Set(updated, tree_value(0x22))],
    );

    let existing_suffix = tree_key(0x20, 0x01);
    let inserted_suffix = tree_key(0x20, 0xc8);
    print_transition_row(
        "insert-absent-suffix",
        vec![(existing_suffix, tree_value(0x21))],
        vec![TreeUpdate::Set(inserted_suffix, tree_value(0x22))],
    );

    let existing_root_branch = tree_key(0x30, 0x01);
    let inserted_root_branch = tree_key(0x40, 0x02);
    print_transition_row(
        "insert-missing-root-stem",
        vec![(existing_root_branch, tree_value(0x31))],
        vec![TreeUpdate::Set(inserted_root_branch, tree_value(0x41))],
    );

    let mut collision_existing = tree_key(0x50, 0x01);
    collision_existing[1] = 0x10;
    let mut collision_inserted = tree_key(0x50, 0x02);
    collision_inserted[1] = 0x20;
    print_transition_row(
        "insert-different-stem-collision",
        vec![(collision_existing, tree_value(0x51))],
        vec![TreeUpdate::Set(collision_inserted, tree_value(0x52))],
    );

    let retained_delete = tree_key(0x60, 0x01);
    let retained_member = tree_key(0x60, 0x02);
    print_transition_row(
        "delete-retained-stem-member",
        vec![
            (retained_delete, tree_value(0x61)),
            (retained_member, tree_value(0x62)),
        ],
        vec![TreeUpdate::Delete(retained_delete)],
    );

    let last_member = tree_key(0x61, 0x01);
    print_transition_row(
        "delete-last-stem-to-empty",
        vec![(last_member, tree_value(0x63))],
        vec![TreeUpdate::Delete(last_member)],
    );

    let mut deepest_left = tree_key(0x70, 0x01);
    deepest_left[2] = 0x01;
    let mut deepest_right = tree_key(0x70, 0x02);
    deepest_right[2] = 0x02;
    print_transition_row(
        "delete-collision-collapse",
        vec![
            (deepest_left, tree_value(0x71)),
            (deepest_right, tree_value(0x72)),
        ],
        vec![TreeUpdate::Delete(deepest_left)],
    );

    let absent_delete_present = tree_key(0x80, 0x01);
    let absent_delete = tree_key(0x90, 0x02);
    print_transition_row(
        "delete-absent-stem-noop",
        vec![(absent_delete_present, tree_value(0x81))],
        vec![TreeUpdate::Delete(absent_delete)],
    );

    let mut replaced = tree_key(0xa0, 0x01);
    replaced[1] = 0x10;
    replaced[2] = 0x10;
    let mut collapsed_sibling = tree_key(0xa0, 0x02);
    collapsed_sibling[1] = 0x40;
    let mut replacement = tree_key(0xa0, 0x03);
    replacement[1] = 0x10;
    replacement[2] = 0x30;
    print_transition_row(
        "mixed-replace-and-collapse",
        vec![
            (replaced, tree_value(0xa1)),
            (collapsed_sibling, tree_value(0xa2)),
        ],
        vec![
            TreeUpdate::Delete(collapsed_sibling),
            TreeUpdate::Set(replacement, tree_value(0xa3)),
            TreeUpdate::Delete(replaced),
        ],
    );

    let present_zero = tree_key(0xb0, 0x01);
    let absent_suffix = tree_key(0xb0, 0x02);
    print_transition_row(
        "set-present-zero-and-delete-absent",
        vec![(present_zero, tree_value(0xb1))],
        vec![
            TreeUpdate::Delete(absent_suffix),
            TreeUpdate::Set(present_zero, [0_u8; 32]),
        ],
    );

    print_generated_transitions();
}

fn topology_key(stem: [u8; 31]) -> [u8; 32] {
    let mut key = [0_u8; 32];
    key[..31].copy_from_slice(&stem);
    key
}

fn print_topology_case(case: &str, stems: Vec<[u8; 31]>, query: [u8; 31]) {
    let mut trie = Trie::new(VerkleConfig::new(MemoryDb::new()));
    trie.insert(
        stems
            .iter()
            .copied()
            .enumerate()
            .map(|(index, stem)| (topology_key(stem), tree_value(index as u8 + 1))),
    );

    let query_key = topology_key(query);
    let proof = trie
        .create_verkle_proof(std::iter::once(query_key))
        .expect("topology proof creation failed");
    assert_eq!(proof.verification_hint.depths.len(), 1);
    assert_eq!(proof.verification_hint.extension_present.len(), 1);
    let depth = proof.verification_hint.depths[0];
    let status = proof.verification_hint.extension_present[0];
    let existing = match status {
        verkle_trie::proof::ExtPresent::None => "-".to_owned(),
        verkle_trie::proof::ExtPresent::Present => encode_hex(&query),
        verkle_trie::proof::ExtPresent::DifferentStem => {
            assert_eq!(proof.verification_hint.diff_stem_no_proof.len(), 1);
            encode_hex(
                proof
                    .verification_hint
                    .diff_stem_no_proof
                    .first()
                    .expect("missing encountered stem"),
            )
        }
    };
    let inserted = if stems.is_empty() {
        "-".to_owned()
    } else {
        stems
            .iter()
            .map(|stem| encode_hex(stem))
            .collect::<Vec<_>>()
            .join(",")
    };

    println!(
        "{case}\t{inserted}\t{}\t{depth}\t{status:?}\t{existing}",
        encode_hex(&query),
    );
}

fn print_topology() {
    let single = [0x10_u8; 31];
    let mut different = single;
    different[1] = 0x11;
    let mut missing_root = single;
    missing_root[0] = 0x12;

    let mut collision_a = [0_u8; 31];
    collision_a[0] = 0x20;
    collision_a[1] = 0x30;
    let mut collision_b = collision_a;
    collision_b[1] = 0x40;
    let mut collision_missing = collision_a;
    collision_missing[1] = 0x35;
    let mut collision_different = collision_a;
    collision_different[2] = 0x01;

    let mut deepest_a = [0_u8; 31];
    deepest_a[30] = 0x01;
    let mut deepest_b = [0_u8; 31];
    deepest_b[30] = 0x02;
    let mut deepest_missing = [0_u8; 31];
    deepest_missing[30] = 0x03;

    println!("case\tinserted_stems\tquery_stem\tdepth\tstatus\texisting_stem");
    print_topology_case("empty", vec![], [0_u8; 31]);
    print_topology_case("single-present", vec![single], single);
    print_topology_case("single-different", vec![single], different);
    print_topology_case("single-missing-root", vec![single], missing_root);
    print_topology_case(
        "collision-present",
        vec![collision_a, collision_b],
        collision_a,
    );
    print_topology_case(
        "collision-missing",
        vec![collision_a, collision_b],
        collision_missing,
    );
    print_topology_case(
        "collision-different",
        vec![collision_a, collision_b],
        collision_different,
    );
    print_topology_case("deepest-present", vec![deepest_a, deepest_b], deepest_a);
    print_topology_case(
        "deepest-missing",
        vec![deepest_a, deepest_b],
        deepest_missing,
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
        Some("commitment-hashes") => print_commitment_hashes(),
        Some("leaf-vectors") => print_leaf_vectors(),
        Some("generators") => print_generators(),
        Some("vector-commitments") => print_vector_commitments(),
        Some("multiproof") => print_multiproof(),
        Some("tree-proof") => print_tree_proof(),
        Some("tree-roots") => print_tree_roots(),
        Some("transitions") => print_transitions(),
        Some("topology") => print_topology(),
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
             <encodings|commitment-hashes|leaf-vectors|generators|vector-commitments|\
             multiproof|tree-proof|tree-roots|transitions|\
             topology|\
             verify-go-witness|\
             update-go-witness>"
        ),
    }
}
