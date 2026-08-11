"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const { CloudEvent, HTTP, Kafka } = require("cloudevents");

const fixtureDirectory = process.argv[2];
const read = (name) => fs.readFileSync(`${fixtureDirectory}/${name}`, "utf8").trim();
const eventFields = [
  "specversion",
  "id",
  "source",
  "type",
  "time",
  "datacontenttype",
  "dataschema",
  "subject",
  "tenantid",
  "traceparent",
  "partitionkey",
  "opaqueext",
];
const assertEvent = (actual, expected) => {
  for (const field of eventFields) {
    assert.equal(actual[field], expected[field], field);
  }
  if (Object.hasOwn(expected, "data_base64")) {
    assert.equal(actual.data_base64, expected.data_base64, "data_base64");
    assert.equal(actual.data, expected.data, "data");
  } else {
    assert.deepEqual(actual.data, expected.data, "data");
  }
};
const context = {
  specversion: "1.0",
  id: "javascript-1",
  source: "/javascript",
  type: "com.example.javascript",
  time: "2026-08-09T00:00:00.000Z",
  datacontenttype: "application/json",
  tenantid: "tenant-a",
  traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
  partitionkey: "partition-a",
  opaqueext: "opaque-value",
  data: { value: 42 },
};
const javascriptEvent = new CloudEvent(context);
assert.equal(JSON.stringify(javascriptEvent), read("javascript-event.json"));
assert.equal(JSON.stringify([javascriptEvent]), read("javascript-batch.json"));

const edgeContext = {
  specversion: "1.0",
  source: "/javascript-edge",
  type: "com.example.edge",
  time: "2026-08-11T00:00:00.000Z",
};
const edgeContexts = [
  { ...edgeContext, id: "absent" },
  { ...edgeContext, id: "null", datacontenttype: "application/json", data: null },
  {
    ...edgeContext,
    id: "empty-text",
    datacontenttype: "text/plain; charset=utf-8",
    data: "",
  },
  {
    ...edgeContext,
    id: "empty-binary",
    datacontenttype: "application/octet-stream",
    data_base64: "",
  },
  {
    ...edgeContext,
    id: "json-parameter",
    datacontenttype: "application/json; charset=utf-8",
    data: { value: 42 },
  },
];
const edgeEvents = edgeContexts.map((edge) => new CloudEvent(edge));
assert.equal(JSON.stringify(edgeEvents), read("javascript-edge-batch.json"));
for (const [index, edgeEvent] of edgeEvents.entries()) {
  let payloadExpected = edgeContexts[index];
  if (index === 1 || index === 2) {
    payloadExpected = { ...edgeContexts[index], data: undefined };
  } else if (index === 3) {
    payloadExpected = { ...edgeContexts[index], data_base64: undefined, data: undefined };
  }
  assertEvent(HTTP.toEvent(HTTP.structured(edgeEvent)), payloadExpected);
  let binaryExpected = payloadExpected;
  if (index === 0) {
    binaryExpected = { ...payloadExpected, datacontenttype: "application/json; charset=utf-8" };
  } else if (index === 1) {
    binaryExpected = { ...edgeContexts[index], data: "null" };
  } else if (index === 2) {
    binaryExpected = edgeContexts[index];
  }
  assertEvent(HTTP.toEvent(HTTP.binary(edgeEvent)), binaryExpected);
}

const golibContext = {
  specversion: "1.0",
  id: "golib-js",
  source: "/golib",
  type: "com.example.golib.javascript",
  time: "2026-08-09T00:00:00Z",
  datacontenttype: "application/json",
  tenantid: "tenant-a",
  traceparent: context.traceparent,
  partitionkey: "partition-a",
  opaqueext: "opaque-value",
  data: { value: 42 },
};
const golibJSON = read("golib-event.json");
const structured = HTTP.toEvent({
  headers: { "content-type": "application/cloudevents+json" },
  body: golibJSON,
});
assertEvent(structured, { ...golibContext, time: "2026-08-09T00:00:00.000Z" });

const batchJSON = read("golib-batch.json");
const batch = HTTP.toEvent({
  headers: { "content-type": "application/cloudevents-batch+json" },
  body: batchJSON,
});
assert.equal(batch.length, 1);
assertEvent(batch[0], golibContext);

const httpBinary = JSON.parse(read("golib-http-binary.json"));
const httpEvent = HTTP.toEvent(httpBinary);
assertEvent(httpEvent, { ...golibContext, time: "2026-08-09T00:00:00.000Z" });

const kafkaBinary = JSON.parse(read("golib-kafka-binary.json"));
kafkaBinary.body = kafkaBinary.value;
const kafkaEvent = Kafka.toEvent(kafkaBinary);
assertEvent(kafkaEvent, { ...golibContext, time: "2026-08-09T00:00:00.000Z" });

const javascriptStructured = HTTP.structured(javascriptEvent);
assertEvent(HTTP.toEvent(javascriptStructured), context);
assert.equal(
  JSON.stringify({ headers: javascriptStructured.headers, body: javascriptStructured.body }),
  read("javascript-http-structured.json"),
);
const javascriptBinary = HTTP.binary(javascriptEvent);
assertEvent(HTTP.toEvent(javascriptBinary), context);
assert.equal(
  JSON.stringify({ headers: javascriptBinary.headers, body: javascriptBinary.body }),
  read("javascript-http-binary.json"),
);
const javascriptKafka = Kafka.structured(javascriptEvent);
assertEvent(Kafka.toEvent(javascriptKafka), context);
assert.equal(
  JSON.stringify({ key: javascriptKafka.key, headers: javascriptKafka.headers, value: javascriptKafka.value }),
  read("javascript-kafka-structured.json"),
);
const javascriptKafkaBinary = Kafka.binary(javascriptEvent);
assertEvent(Kafka.toEvent(javascriptKafkaBinary), context);
const definedKafkaHeaders = Object.fromEntries(
  Object.entries(javascriptKafkaBinary.headers).filter(([, value]) => value !== undefined),
);
assert.equal(
  JSON.stringify({
    key: javascriptKafkaBinary.key,
    headers: definedKafkaHeaders,
    value: JSON.stringify(javascriptKafkaBinary.value),
  }),
  read("javascript-kafka-binary.json"),
);
