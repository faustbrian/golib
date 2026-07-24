# Encoding

`Date` text is exactly ten ASCII bytes in `YYYY-MM-DD`. JSON is exactly a JSON
string containing that text. The zero date, `null`, invalid UTF-8, impossible
dates, non-ASCII digits, trailing input, and years outside 0001–9999 fail.

`calendarwire.Version == 1` identifies this stable canonical contract and its
decoder caps input at 64 bytes. Locale-aware display belongs in a presentation
adapter; never use localized display text as a wire value.
