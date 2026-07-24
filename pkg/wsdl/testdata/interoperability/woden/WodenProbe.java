import java.io.File;
import java.net.URI;
import java.util.Arrays;
import org.apache.woden.WSDLFactory;
import org.apache.woden.WSDLReader;
import org.apache.woden.wsdl20.Description;
import org.apache.woden.wsdl20.Interface;
import org.apache.woden.wsdl20.InterfaceOperation;

public final class WodenProbe {
    private WodenProbe() {}

    public static void main(String[] sources) throws Exception {
        WSDLReader reader = WSDLFactory.newInstance().newWSDLReader();
        reader.setFeature(WSDLReader.FEATURE_VALIDATION, true);

        for (String source : sources) {
            Description description = reader.readWSDL(source);
            if (description == null || description.getInterfaces().length == 0) {
                throw new IllegalStateException("empty WSDL graph: " + source);
            }
            for (Interface interfaceValue : description.getInterfaces()) {
                for (InterfaceOperation operation : interfaceValue.getInterfaceOperations()) {
                    URI[] styles = operation.getStyle();
                    String[] styleNames = new String[styles.length];
                    for (int index = 0; index < styles.length; index++) {
                        styleNames[index] = styles[index].toString();
                    }
                    Arrays.sort(styleNames);
                    System.out.printf(
                        "%s\t%s\t%s\t%s\t%s\t%s%n",
                        new File(source).getName(),
                        interfaceValue.getName().getNamespaceURI(),
                        interfaceValue.getName().getLocalPart(),
                        operation.getName().getLocalPart(),
                        operation.getMessageExchangePattern(),
                        String.join(",", styleNames)
                    );
                }
            }
        }
    }
}
