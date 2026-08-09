"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const { CloudEvent, HTTP, Kafka } = require("cloudevents");

const fixtureDirectory = process.argv[2];
const read = (name) => fs.readFileSync(`${fixtureDirectory}/${name}`, "utf8").trim();
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
  data: { value: 42 },
};
const javascriptEvent = new CloudEvent(context);
assert.equal(JSON.stringify(javascriptEvent), read("javascript-event.json"));

const golibJSON = read("golib-event.json");
const structured = HTTP.toEvent({
  headers: { "content-type": "application/cloudevents+json" },
  body: golibJSON,
});
assert.equal(structured.id, "golib-js");
assert.equal(structured.tenantid, "tenant-a");
assert.equal(structured.traceparent, context.traceparent);
assert.equal(structured.partitionkey, "partition-a");
assert.deepEqual(structured.data, { value: 42 });

const batchJSON = read("golib-batch.json");
const batch = HTTP.toEvent({
  headers: { "content-type": "application/cloudevents-batch+json" },
  body: batchJSON,
});
assert.equal(batch.length, 1);
assert.equal(batch[0].id, "golib-js");

const httpBinary = JSON.parse(read("golib-http-binary.json"));
const httpEvent = HTTP.toEvent(httpBinary);
assert.equal(httpEvent.id, "golib-js");
assert.equal(httpEvent.partitionkey, "partition-a");
assert.deepEqual(httpEvent.data, { value: 42 });

const kafkaBinary = JSON.parse(read("golib-kafka-binary.json"));
kafkaBinary.body = kafkaBinary.value;
const kafkaEvent = Kafka.toEvent(kafkaBinary);
assert.equal(kafkaEvent.id, "golib-js");
assert.equal(kafkaEvent.partitionkey, "partition-a");
assert.deepEqual(kafkaEvent.data, { value: 42 });

const javascriptStructured = HTTP.structured(javascriptEvent);
assert.equal(HTTP.toEvent(javascriptStructured).id, "javascript-1");
const javascriptKafka = Kafka.structured(javascriptEvent);
assert.equal(Kafka.toEvent(javascriptKafka).id, "javascript-1");
