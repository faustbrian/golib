package wsdl_test

import (
	"context"
	"errors"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestMarshalLimitAppliesAtEveryOutputBoundary(t *testing.T) {
	t.Parallel()

	for _, source := range []string{serializationWSDL11, serializationWSDL20} {
		document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{
			SystemID: "https://example.test/wsdl/root.wsdl",
		})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		full, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		for maximum := 1; maximum < len(full); maximum++ {
			_, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: int64(maximum)})
			if !errors.Is(err, wsdl.ErrLimitExceeded) {
				t.Fatalf("Marshal(MaxBytes: %d) error = %v", maximum, err)
			}
		}
	}
}

const serializationWSDL11 = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
	` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
	` xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"` +
	` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
	` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/"` +
	` xmlns:ext="urn:extension" name="Complete" targetNamespace="urn:test"` +
	` ext:flag="present"><documentation xml:lang="en">Complete fixture</documentation>` +
	`<import namespace="urn:other" location="other.wsdl"><documentation>Import</documentation>` +
	`<ext:policy/></import><types><documentation>Types</documentation>` +
	`<xs:schema targetNamespace="urn:test"><xs:element name="Value" type="xs:string"/>` +
	`</xs:schema></types><message name="Request"><documentation>Request</documentation>` +
	`<part name="body" element="tns:Value"><documentation>Part</documentation></part>` +
	`</message><message name="Response"><part name="value" type="xs:string"/></message>` +
	`<portType name="API"><documentation>API</documentation><operation name="Call"` +
	` parameterOrder="body value"><documentation>Call</documentation>` +
	`<input name="Request" message="tns:Request"><documentation>Input</documentation></input>` +
	`<output name="Response" message="tns:Response"><documentation>Output</documentation></output>` +
	`<fault name="Failure" message="tns:Response"><documentation>Fault</documentation></fault>` +
	`</operation></portType><binding name="SOAP" type="tns:API">` +
	`<documentation>SOAP</documentation><soap12:binding style="rpc" transport="urn:soap"/>` +
	`<operation name="Call"><documentation>Bound call</documentation>` +
	`<soap12:operation soapAction="urn:call" soapActionRequired="true" style="document"/>` +
	`<input name="Request"><documentation>Bound input</documentation>` +
	`<soap12:body use="encoded" namespace="urn:body" encodingStyle="urn:one urn:two"` +
	` parts="body"/><soap12:header message="tns:Request" part="body" use="literal"` +
	` namespace="urn:header" encodingStyle="urn:header-encoding">` +
	`<soap12:headerfault message="tns:Response" part="value" use="encoded"` +
	` namespace="urn:header-fault" encodingStyle="urn:fault-encoding"/>` +
	`</soap12:header></input><output name="Response"><soap12:body use="literal"/>` +
	`</output><fault name="Failure"><soap12:fault name="Failure" use="literal"` +
	` namespace="urn:fault" encodingStyle="urn:fault-encoding"/></fault></operation>` +
	`</binding><binding name="HTTP" type="tns:API"><http:binding verb="POST"/>` +
	`<operation name="Call"><http:operation location="/call/(body)"/>` +
	`<input name="Request"><http:urlEncoded/><http:urlReplacement/>` +
	`<mime:multipartRelated><mime:part><soap12:body use="literal"/>` +
	`<mime:content part="body" type="application/xml"/><mime:mimeXml part="body"/>` +
	`</mime:part></mime:multipartRelated></input><output name="Response">` +
	`<mime:content part="value" type="text/plain"/><mime:mimeXml part="value"/>` +
	`</output></operation></binding><service name="Service"><documentation>Service</documentation>` +
	`<port name="SOAPPort" binding="tns:SOAP"><documentation>SOAP port</documentation>` +
	`<soap12:address location="https://example.test/soap"/></port>` +
	`<port name="HTTPPort" binding="tns:HTTP"><http:address` +
	` location="https://example.test/http"/></port></service><ext:root/></definitions>`

const serializationWSDL20 = `<description xmlns="http://www.w3.org/ns/wsdl"` +
	` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
	` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
	` xmlns:whttp="http://www.w3.org/ns/wsdl/http" xmlns:env="urn:env"` +
	` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"` +
	` xmlns:ext="urn:extension" targetNamespace="urn:test" ext:flag="present">` +
	`<documentation xml:lang="en">Complete fixture</documentation>` +
	`<import namespace="urn:other" location="other.wsdl"><documentation>Import</documentation>` +
	`</import><include location="included.wsdl"><documentation>Include</documentation></include>` +
	`<types><documentation>Types</documentation><xs:schema targetNamespace="urn:test">` +
	`<xs:element name="Request" type="xs:string"/><xs:element name="Response"` +
	` type="xs:string"/><xs:element name="Failure" type="xs:string"/>` +
	`</xs:schema></types><interface name="API" styleDefault="urn:style">` +
	`<documentation>API</documentation><fault name="Failure" element="tns:Failure">` +
	`<documentation>Failure</documentation></fault><operation name="Call"` +
	` pattern="http://www.w3.org/ns/wsdl/in-out" wsdlx:safe="true" style="urn:operation-style">` +
	`<documentation>Call</documentation><input messageLabel="In" element="tns:Request">` +
	`<documentation>Input</documentation></input><output messageLabel="Out"` +
	` element="tns:Response"><documentation>Output</documentation></output>` +
	`<outfault ref="tns:Failure" messageLabel="Out"><documentation>Out fault</documentation>` +
	`</outfault></operation></interface><binding name="SOAP" interface="tns:API"` +
	` type="http://www.w3.org/ns/wsdl/soap" wsoap:version="1.2"` +
	` wsoap:protocol="urn:protocol" wsoap:mepDefault="urn:mep">` +
	`<documentation>SOAP</documentation><wsoap:module ref="urn:module" required="true"/>` +
	`<fault ref="tns:Failure" wsoap:code="env:Sender" wsoap:subcodes="#any">` +
	`<wsoap:module ref="urn:fault-module"/><wsoap:header element="tns:Request"` +
	` mustUnderstand="true" required="false"/></fault><operation ref="tns:Call"` +
	` wsoap:mep="urn:operation-mep" wsoap:action="urn:action">` +
	`<wsoap:module ref="urn:operation-module"/><input messageLabel="In">` +
	`<wsoap:module ref="urn:input-module"/><wsoap:header element="tns:Request"` +
	` mustUnderstand="false" required="true"/></input><output messageLabel="Out"/>` +
	`<outfault ref="tns:Failure" messageLabel="Out"><wsoap:module ref="urn:reference-module"/>` +
	`</outfault></operation></binding><binding name="HTTP" interface="tns:API"` +
	` type="http://www.w3.org/ns/wsdl/http" whttp:methodDefault="POST"` +
	` whttp:queryParameterSeparatorDefault=";" whttp:cookies="true"` +
	` whttp:contentEncodingDefault="gzip"><fault ref="tns:Failure" whttp:code="500"` +
	` whttp:contentEncoding="br"><whttp:header name="Retry-After" type="xs:string"` +
	` required="false"/></fault><operation ref="tns:Call" whttp:location="items/{id}"` +
	` whttp:method="PUT" whttp:inputSerialization="application/json"` +
	` whttp:outputSerialization="application/json"` +
	` whttp:faultSerialization="application/problem+json"` +
	` whttp:queryParameterSeparator="&amp;" whttp:contentEncodingDefault="identity"` +
	` whttp:ignoreUncited="true"><input messageLabel="In" whttp:contentEncoding="gzip">` +
	`<whttp:header name="X-Request-ID" type="xs:string" required="true"/></input>` +
	`<output messageLabel="Out" whttp:contentEncoding="br"/>` +
	`<outfault ref="tns:Failure" messageLabel="Out"/></operation></binding>` +
	`<service name="Service" interface="tns:API"><documentation>Service</documentation>` +
	`<endpoint name="SOAPEndpoint" binding="tns:SOAP" address="https://example.test/soap"/>` +
	`<endpoint name="HTTPEndpoint" binding="tns:HTTP" address="https://example.test/http"` +
	` whttp:authenticationScheme="basic" whttp:authenticationRealm="api"/>` +
	`</service><ext:root/></description>`
