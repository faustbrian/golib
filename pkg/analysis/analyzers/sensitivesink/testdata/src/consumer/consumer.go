package consumer

import (
	secrets "secretmodel"
	"sinkapi"
)

func Direct(token secrets.Token, credentials secrets.Credentials) {
	sinkapi.Record(token)                                    // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.Record argument 1`
	sinkapi.Record(&token)                                   // want `security/sensitive-sink: \*secretmodel.Token flows to sinkapi.Record argument 1`
	sinkapi.Record([]secrets.Token{token})                   // want `security/sensitive-sink: \[\]secretmodel.Token flows to sinkapi.Record argument 1`
	sinkapi.Record([1]secrets.Token{token})                  // want `security/sensitive-sink: \[1\]secretmodel.Token flows to sinkapi.Record argument 1`
	sinkapi.Record(map[string]secrets.Token{"token": token}) // want `security/sensitive-sink: map\[string\]secretmodel.Token flows to sinkapi.Record argument 1`
	sinkapi.Record(credentials)                              // want `security/sensitive-sink: secretmodel.Credentials flows to sinkapi.Record argument 1`
	_ = sinkapi.Format("%v", token)                          // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.Format argument 2`
	logger := &sinkapi.Logger{}
	logger.Log("token", token)                 // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.Logger.Log argument 2`
	sinkapi.All(token)                         // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.All argument 1`
	sinkapi.RecordThree("safe", "safe", token) // want `security/sensitive-sink: secretmodel.Token flows to sinkapi.RecordThree argument 3`
}

func NearMiss(token secrets.Token) {
	var err error
	sinkapi.Record(err)
	sinkapi.Record(string(token))
	sinkapi.Record("safe")
	_ = sinkapi.Format("%s", "safe")
	sinkapi.Current(token)
	sinkapi.Record(secrets.Public("safe"))
	sinkapi.Capture[secrets.Token](token)
	sinkapi.CapturePair[secrets.Token, string](token, "safe")
	sinkapi.RecordThree("safe", token, "safe")
	callback := func(any) {}
	callback(token)
	func(any) {}(token)
}
