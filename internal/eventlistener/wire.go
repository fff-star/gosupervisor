package eventlistener

import (
	"fmt"

	"gosupervisor/internal/process"
)

// supervisordEventDerives maps supervisord event group names to the internal
// EventTypes they subsume. Used to match listener subscriptions.
var supervisordEventDerives = map[string][]process.EventType{
	"EVENT": {
		process.EventStart, process.EventStop, process.EventExit,
		process.EventFatal, process.EventHealthFail, process.EventHealthRestore,
		process.EventSignal,
	},
	"PROCESS_STATE": {
		process.EventStart, process.EventStop, process.EventExit,
		process.EventFatal, process.EventHealthFail, process.EventHealthRestore,
	},
	"PROCESS_STATE_STARTING": {process.EventStart},
	"PROCESS_STATE_RUNNING":  {process.EventHealthRestore},
	"PROCESS_STATE_STOPPED":  {process.EventStop},
	"PROCESS_STATE_EXITED":   {process.EventExit},
	"PROCESS_STATE_FATAL":    {process.EventFatal, process.EventHealthFail},
	"PROCESS_STATE_STOPPING": {},
	"PROCESS_STATE_BACKOFF":  {},
	"PROCESS_STATE_UNKNOWN":  {},
	"PROCESS_LOG_STDOUT":     {},
	"PROCESS_LOG_STDERR":     {},
	"PROCESS_COMMUNICATION":  {},
	"TICK_5":                 {},
	"TICK_60":                {},
	"TICK_3600":              {},
}

// eventWireName returns the supervisord wire-format event name for an internal EventType.
func eventWireName(typ process.EventType) string {
	switch typ {
	case process.EventStart:
		return "PROCESS_STATE_STARTING"
	case process.EventStop:
		return "PROCESS_STATE_STOPPED"
	case process.EventExit:
		return "PROCESS_STATE_EXITED"
	case process.EventFatal:
		return "PROCESS_STATE_FATAL"
	case process.EventHealthFail:
		return "PROCESS_STATE_FATAL"
	case process.EventHealthRestore:
		return "PROCESS_STATE_RUNNING"
	case process.EventSignal:
		return "PROCESS_STATE"
	default:
		return "UNKNOWN"
	}
}

// encodeEvent encodes a process event into the supervisord wire format:
//
//	ver:3.0 server:<serverID> serial:<serial> pool:<pool> poolserial:<poolSerial> eventname:<type> len:<N>\n
//	<body bytes>
func encodeEvent(event process.Event, serverID string, serial, poolSerial int64, pool string) []byte {
	body := fmt.Sprintf("processname:%s groupname:%s pid:%d\n", event.Name, event.Name, event.PID)
	eventName := eventWireName(event.Type)
	header := fmt.Sprintf(
		"ver:3.0 server:%s serial:%d pool:%s poolserial:%d eventname:%s len:%d\n",
		serverID, serial, pool, poolSerial, eventName, len(body),
	)
	buf := make([]byte, 0, len(header)+len(body))
	buf = append(buf, []byte(header)...)
	buf = append(buf, []byte(body)...)
	return buf
}
