package xsdtest.reference;

import java.io.File;
import javax.xml.XMLConstants;
import javax.xml.transform.stream.StreamSource;
import javax.xml.validation.Schema;
import javax.xml.validation.SchemaFactory;
import org.xml.sax.SAXException;

public final class ReferenceBenchmark {
    private ReferenceBenchmark() {}

    public static void main(String[] arguments) throws Exception {
        if (arguments.length != 4) {
            throw new IllegalArgumentException(
                "usage: ReferenceBenchmark schema valid invalid iterations"
            );
        }

        File schemaFile = new File(arguments[0]);
        File validFile = new File(arguments[1]);
        File invalidFile = new File(arguments[2]);
        int iterations = Integer.parseInt(arguments[3]);
        if (iterations < 1) {
            throw new IllegalArgumentException("iterations must be positive");
        }

        SchemaFactory factory = schemaFactory();
        Schema schema = factory.newSchema(schemaFile);
        validate(schema, validFile);
        expectInvalid(schema, invalidFile);

        runCompile(factory, schemaFile, Math.max(5, iterations / 10));
        runValidate(schema, validFile, Math.max(5, iterations / 10));

        long compileStart = System.nanoTime();
        for (int index = 0; index < iterations; index++) {
            factory.newSchema(schemaFile);
        }
        long compileNanos = System.nanoTime() - compileStart;

        long validateStart = System.nanoTime();
        runValidate(schema, validFile, iterations);
        long validateNanos = System.nanoTime() - validateStart;

        System.out.printf(
            "ReferenceCompileSchemaJAXP %d ns/op%n",
            compileNanos / iterations
        );
        System.out.printf(
            "ReferenceValidateInstanceJAXP %d ns/op%n",
            validateNanos / iterations
        );
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

    private static void runCompile(
        SchemaFactory factory,
        File schemaFile,
        int iterations
    ) throws SAXException {
        for (int index = 0; index < iterations; index++) {
            factory.newSchema(schemaFile);
        }
    }

    private static void runValidate(
        Schema schema,
        File instanceFile,
        int iterations
    ) throws Exception {
        for (int index = 0; index < iterations; index++) {
            validate(schema, instanceFile);
        }
    }

    private static void validate(Schema schema, File instanceFile)
        throws Exception {
        schema.newValidator().validate(new StreamSource(instanceFile));
    }

    private static void expectInvalid(Schema schema, File instanceFile)
        throws Exception {
        try {
            validate(schema, instanceFile);
        } catch (SAXException expected) {
            return;
        }
        throw new IllegalStateException(
            "reference engine accepted the invalid benchmark instance"
        );
    }
}
