import com.amazonaws.services.schemaregistry.common.configs.GlueSchemaRegistryConfiguration;
import com.amazonaws.services.schemaregistry.serializers.SerializationDataEncoder;

import java.util.HexFormat;
import java.util.UUID;

public final class GlueWireReference {
    private GlueWireReference() {}

    public static void main(String[] args) {
        if (args.length != 2 && args.length != 3) {
            throw new IllegalArgumentException("expected schema UUID, payload hex, and optional benchmark iterations");
        }
        var configuration = new GlueSchemaRegistryConfiguration("us-east-1");
        var encoder = new SerializationDataEncoder(configuration);
        byte[] frame = encoder.write(HexFormat.of().parseHex(args[1]), UUID.fromString(args[0]));
        System.out.print(HexFormat.of().formatHex(frame));
        if (args.length == 3) {
            int iterations = Integer.parseInt(args[2]);
            byte[] benchmarkPayload = new byte[1024];
            UUID schemaId = UUID.fromString(args[0]);
            for (int index = 0; index < 10_000; index++) {
                encoder.write(benchmarkPayload, schemaId);
            }
            long started = System.nanoTime();
            for (int index = 0; index < iterations; index++) {
                encoder.write(benchmarkPayload, schemaId);
            }
            long elapsed = System.nanoTime() - started;
            System.out.printf("%nofficial_aws_java_serde ns/op=%.2f iterations=%d payload_bytes=%d%n",
                (double) elapsed / iterations, iterations, benchmarkPayload.length);
        }
    }
}
