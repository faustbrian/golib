import io.confluent.kafka.serializers.schema.id.PrefixSchemaIdSerializer;
import io.confluent.kafka.serializers.schema.id.SchemaId;
import java.util.HexFormat;
import java.util.List;
import java.util.Locale;

public final class ConfluentWireReference {
    private static final int BENCHMARK_PAYLOAD_BYTES = 1024;
    private static final int WARMUP_ITERATIONS = 10_000;

    private ConfluentWireReference() {
    }

    public static void main(String[] args) {
        if (args.length != 3) {
            throw new IllegalArgumentException("expected schema ID, payload hex, and benchmark iterations");
        }
        int id = Integer.parseInt(args[0]);
        byte[] payload = HexFormat.of().parseHex(args[1]);
        int iterations = Integer.parseInt(args[2]);
        PrefixSchemaIdSerializer serializer = new PrefixSchemaIdSerializer();
        SchemaId classic = new SchemaId("AVRO", id, (String) null);
        SchemaId protobuf = new SchemaId("PROTOBUF", id, (String) null);
        protobuf.setMessageIndexes(List.of(0));

        System.out.println(HexFormat.of().formatHex(
            serializer.serialize("reference", false, null, payload, classic)));
        System.out.println(HexFormat.of().formatHex(
            serializer.serialize("reference", false, null, payload, protobuf)));
        benchmark(serializer, classic, "classic", iterations);
        benchmark(serializer, protobuf, "protobuf", iterations);
    }

    private static void benchmark(
        PrefixSchemaIdSerializer serializer,
        SchemaId schemaId,
        String format,
        int iterations
    ) {
        byte[] payload = new byte[BENCHMARK_PAYLOAD_BYTES];
        int observed = 0;
        for (int index = 0; index < WARMUP_ITERATIONS; index++) {
            observed += serializer.serialize("reference", false, null, payload, schemaId).length;
        }
        observed = 0;
        long started = System.nanoTime();
        for (int index = 0; index < iterations; index++) {
            observed += serializer.serialize("reference", false, null, payload, schemaId).length;
        }
        long elapsed = System.nanoTime() - started;
        System.out.printf(Locale.ROOT,
            "official_confluent_java_schema_id_serializer_%s ns/op=%.2f iterations=%d payload_bytes=%d observed=%d%n",
            format, (double) elapsed / iterations, iterations, payload.length, observed);
    }
}
