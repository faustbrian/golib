import com.amazonaws.services.schemaregistry.common.configs.GlueSchemaRegistryConfiguration;
import com.amazonaws.services.schemaregistry.serializers.SerializationDataEncoder;

import java.util.HexFormat;
import java.util.UUID;

public final class GlueWireReference {
    private GlueWireReference() {}

    public static void main(String[] args) {
        if (args.length != 2) {
            throw new IllegalArgumentException("expected schema UUID and payload hex");
        }
        var configuration = new GlueSchemaRegistryConfiguration("us-east-1");
        var encoder = new SerializationDataEncoder(configuration);
        byte[] frame = encoder.write(HexFormat.of().parseHex(args[1]), UUID.fromString(args[0]));
        System.out.print(HexFormat.of().formatHex(frame));
    }
}
