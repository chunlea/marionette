# gRPC Protocol

The agent-to-server communication protocol.

## Overview

Agents communicate with the server using bidirectional gRPC streaming:

=== "Diagram"

    ```mermaid
    sequenceDiagram
        participant Agent as Agent (gRPC Client)
        participant Server as Server (gRPC Server)

        Agent->>Server: Connect()
        loop Bidirectional Streaming
            Server-->>Agent: Control (commands)
            Agent-->>Server: Events (logs, etc)
        end
    ```

=== "Text"

    ```
    ┌──────────────────┐                    ┌──────────────────┐
    │      Agent       │                    │      Server      │
    │                  │                    │                  │
    │  ┌────────────┐  │     Connect()      │  ┌────────────┐  │
    │  │ gRPC Client├──┼───────────────────►│  │ gRPC Server│  │
    │  └────────────┘  │                    │  └────────────┘  │
    │                  │◄─── Control ───────│                  │
    │                  │     (commands)     │                  │
    │                  │                    │                  │
    │                  │──── Events ───────►│                  │
    │                  │     (logs, etc)    │                  │
    └──────────────────┘                    └──────────────────┘
    ```

## Service Definition

```protobuf
service RunnerService {
  // Bidirectional streaming for runner control
  rpc Connect(stream RunnerEvent) returns (stream ControlMessage);

  // Log streaming (high-throughput)
  rpc StreamLogs(stream LogEntry) returns (LogAck);
}
```

## Messages

### Runner Events (Agent → Server)

```protobuf
message RunnerEvent {
  oneof event {
    RegisterRequest register = 1;
    Heartbeat heartbeat = 2;
    TaskStarted task_started = 3;
    TaskCompleted task_completed = 4;
    TaskFailed task_failed = 5;
    PermissionRequest permission_request = 6;
    StatusUpdate status_update = 7;
  }
}

message RegisterRequest {
  string runner_id = 1;
  string token = 2;
  string hostname = 3;
  repeated string capabilities = 4;
  map<string, string> labels = 5;
}

message PermissionRequest {
  string request_id = 1;
  string task_id = 2;
  string tool = 3;
  string action = 4;
  string context = 5;
  string risk_level = 6;
}
```

### Control Messages (Server → Agent)

```protobuf
message ControlMessage {
  oneof message {
    AssignTask assign_task = 1;
    CancelTask cancel_task = 2;
    PermissionResponse permission_response = 3;
    Shutdown shutdown = 4;
    Ping ping = 5;
  }
}

message AssignTask {
  string task_id = 1;
  string session_id = 2;
  string prompt = 3;
  AgentConfig agent_config = 4;
  int32 timeout_seconds = 5;
}

message PermissionResponse {
  string request_id = 1;
  bool approved = 2;
  string reason = 3;
}
```

### Log Entries

```protobuf
message LogEntry {
  string task_id = 1;
  string run_id = 2;
  string stream = 3;  // stdout, stderr
  string content = 4;
  int64 sequence = 5;
  google.protobuf.Timestamp timestamp = 6;
}
```

## Connection Flow

```
Agent                              Server
  │                                  │
  │──── RegisterRequest ────────────►│
  │     (runner_id, token, caps)     │
  │                                  │
  │◄─── RegisterAck ─────────────────│
  │     (accepted, session_id)       │
  │                                  │
  │◄─── AssignTask ──────────────────│
  │     (task_id, prompt, config)    │
  │                                  │
  │──── TaskStarted ────────────────►│
  │                                  │
  │──── LogEntry ───────────────────►│
  │──── LogEntry ───────────────────►│
  │                                  │
  │──── PermissionRequest ──────────►│
  │     (tool, action)               │
  │                                  │
  │◄─── PermissionResponse ──────────│
  │     (approved/denied)            │
  │                                  │
  │──── TaskCompleted ──────────────►│
  │     (exit_code, result)          │
  │                                  │
  │◄─── Ping ────────────────────────│
  │──── Heartbeat ──────────────────►│
  │                                  │
```

## Reconnection

Agents automatically reconnect on connection loss:

```
1. Connection lost
2. Exponential backoff (1s, 2s, 4s, ... max 60s)
3. Re-register with same runner_id
4. Server resumes any pending tasks
```

## Authentication

### mTLS (Production)

```bash
./bin/agent \
  --server marionette.example.com:9090 \
  --tls-cert /etc/tls/agent-cert.pem \
  --tls-key /etc/tls/agent-key.pem \
  --tls-ca /etc/tls/ca-cert.pem
```

### Token (Development)

```bash
./bin/agent \
  --server localhost:9090 \
  --token rtok_xxx
```

## Full Proto Definition

See the complete protocol definition:

[`docs/runner.proto`](https://github.com/chunlea/marionette/blob/main/docs/runner.proto)
