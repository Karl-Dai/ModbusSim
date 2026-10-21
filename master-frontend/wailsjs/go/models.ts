export namespace main {

	export interface FileFilter {
	    Name: string;
	    Extensions: string[];
	}
	export interface PLCAddress {
	    ProtocolAddress: number;
	    RegisterType: string;
	}

}

export namespace masterstate {

	export interface ConnectionInfo {
	    ID: string;
	    TargetAddress: string;
	    Port: number;
	    SlaveID: number;
	    State: string;
	    ScanGroupCount: number;
	}
	export interface LogEntry {
	    Timestamp: string;
	    Direction: string;
	    FunctionCode: string;
	    Detail: string;
	}
	export interface RegisterValue {
	    Address: number;
	    RawValue: number;
	    DisplayValue: string;
	    IsBool: boolean;
	}
	export interface ReadResult {
	    ScanGroupID: string;
	    Function: string;
	    StartAddress: number;
	    Values: RegisterValue[];
	    Timestamp: string;
	    Error?: string;
	}
	export interface RegisterScanRequest {
	    Function: string;
	    StartAddress: number;
	    EndAddress: number;
	    ChunkSize: number;
	    TimeoutMS: number;
	}

	export interface ScanGroup {
	    ID: string;
	    Name: string;
	    Function: string;
	    StartAddress: number;
	    Quantity: number;
	    IntervalMS: number;
	    Enabled: boolean;
	    IsPolling: boolean;
	    SlaveID?: number;
	}
	export interface Settings {
	    Transport: project.Transport;
	    SlaveID: number;
	    TimeoutMS: number;
	    Requests: project.RequestSettings;
	    Reconnect: project.ReconnectPolicy;
	    Socks5?: project.Socks5Config;
	}
	export interface SlaveScanRequest {
	    StartID: number;
	    EndID: number;
	    TimeoutMS: number;
	}

}

export namespace project {

	export interface ClientTLS {
	    Enabled: boolean;
	    CAFile?: string;
	    CertFile?: string;
	    KeyFile?: string;
	    PKCS12File?: string;
	    PKCS12Password?: string;
	    AcceptInvalidCerts?: boolean;
	}
	export interface ReconnectPolicy {
	    Enabled: boolean;
	    InitialDelayMS: number;
	    MaxDelayMS: number;
	    BackoffFactor: number;
	    MaxAttempts?: number;
	}
	export interface RequestSettings {
	    IntervalMS: number;
	    MaxReadRegisters: number;
	    MaxReadBits: number;
	}
	export interface SerialConfig {
	    Port: string;
	    BaudRate: number;
	    DataBits: number;
	    StopBits: number;
	    Parity: string;
	}
	export interface ServerTLS {
	    Enabled: boolean;
	    CertFile?: string;
	    KeyFile?: string;
	    CAFile?: string;
	    RequireClientCert?: boolean;
	    PKCS12File?: string;
	    PKCS12Password?: string;
	}
	export interface Socks5Config {
	    Host: string;
	    Port: number;
	    Username?: string;
	    Password?: string;
	}
	export interface Transport {
	    Type: string;
	    Host?: string;
	    Port?: number;
	    Serial?: SerialConfig;
	    TLS?: ClientTLS;
	    ServerTLS?: ServerTLS;
	}

}
