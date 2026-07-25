package xsdtest.reference;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import javax.xml.XMLConstants;
import javax.xml.transform.stream.StreamSource;
import javax.xml.validation.Schema;
import javax.xml.validation.SchemaFactory;
import org.xml.sax.SAXException;

public final class ReferenceDifferential {
    private ReferenceDifferential() {}

    public static void main(String[] arguments) throws Exception {
        if (arguments.length != 3) {
            throw new IllegalArgumentException(
                "usage: ReferenceDifferential schema manifest root"
            );
        }
        Path root = Path.of(arguments[2]).toAbsolutePath().normalize();
        SchemaFactory factory = schemaFactory();
        Schema schema = factory.newSchema(confined(root, arguments[0]).toFile());
        List<String> lines = Files.readAllLines(confined(root, arguments[1]));
        int passed = 0;
        for (String line : lines) {
            String[] fields = line.split("\\t", -1);
            if (fields.length != 3 || fields[0].equals("id")) {
                continue;
            }
            boolean expected = fields[1].equals("valid");
            boolean actual = validates(schema, confined(root, fields[2]));
            if (actual != expected) {
                throw new IllegalStateException(
                    fields[0] + ": expected " + fields[1]
                );
            }
            passed++;
        }
        if (passed == 0) {
            throw new IllegalStateException("no differential cases executed");
        }
        System.out.printf("JAXP differential passed=%d failed=0 skipped=0%n", passed);
    }

    private static SchemaFactory schemaFactory() throws Exception {
        SchemaFactory factory = SchemaFactory.newInstance(
            XMLConstants.W3C_XML_SCHEMA_NS_URI
        );
        factory.setFeature(XMLConstants.FEATURE_SECURE_PROCESSING, true);
        factory.setProperty(XMLConstants.ACCESS_EXTERNAL_DTD, "");
        factory.setProperty(XMLConstants.ACCESS_EXTERNAL_SCHEMA, "");
        return factory;
    }

    private static boolean validates(Schema schema, Path instance)
        throws IOException {
        try {
            schema.newValidator().validate(new StreamSource(instance.toFile()));
            return true;
        } catch (SAXException expected) {
            return false;
        }
    }

    private static Path confined(Path root, String relative) {
        Path path = root.resolve(relative).normalize();
        if (!path.startsWith(root)) {
            throw new IllegalArgumentException("path escapes differential root");
        }
        return path;
    }
}
