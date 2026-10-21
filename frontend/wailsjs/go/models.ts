export namespace appstate {

	export interface AddDeviceRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    Name: string;
	    InitMode: string;
	}
	export interface AddRegisterRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    Address: number;
	    RegisterType: string;
	    DataType: string;
	    Endian: string;
	    Name: string;
	    Comment: string;
	}
	export interface CreateConnectionRequest {
	    Transport: project.Transport;
	    InitMode: string;
	    SlaveID: number;
	    Name: string;
	}
	export interface DeviceInfo {
	    SlaveID: number;
	    Name: string;
	    RegisterCount: number;
	}
	export interface ImportRegistersRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    Points: project.Point[];
	    Mode: string;
	}
	export interface PointMutationInfo {
	    RegisterType: string;
	    Address: number;
	    Mode: string;
	    PeriodMS: number;
	    Step: number;
	    Min: number;
	    Max: number;
	}
	export interface RegisterValueInfo {
	    Address: number;
	    Value: number;
	}
	export interface UpdateDeviceRequest {
	    ConnectionID: string;
	    OriginalSlaveID: number;
	    SlaveID: number;
	    Name: string;
	}
	export interface UpdateRegisterRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    OriginalAddress: number;
	    OriginalRegisterType: string;
	    Address: number;
	    RegisterType: string;
	    DataType: string;
	    Endian: string;
	    Name: string;
	    Comment: string;
	}
	export interface WriteRegisterRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    RegisterType: string;
	    Address: number;
	    Value: number;
	}

}

export namespace main {

	export interface ClientInfo {
	    ID: number;
	    PeerAddress: string;
	    ConnectedAt: string;
	}
	export interface ConnectionInfo {
	    ID: string;
	    BindAddress: string;
	    Port: number;
	    State: string;
	    DeviceCount: number;
	    Clients: ClientInfo[];
	}
	export interface FileFilter {
	    Name: string;
	    Extensions: string[];
	}
	export interface LogEntry {
	    Timestamp: string;
	    Direction: string;
	    FunctionCode: string;
	    Detail: string;
	}
	export interface PLCAddress {
	    ProtocolAddress: number;
	    RegisterType: string;
	}
	export interface Point {
	    Address: number;
	    RegisterType: string;
	    DataType: string;
	    Endian: string;
	    Name: string;
	    Comment: string;
	    Mutation?: project.MutationConfig;
	    DataSource?: project.DataSourceConfig;
	}
	export interface SetDataSourceRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    RegisterType: string;
	    Address: number;
	    Source: project.DataSource;
	    UpdateIntervalMS: number;
	}
	export interface SetPointMutationRequest {
	    ConnectionID: string;
	    SlaveID: number;
	    RegisterType: string;
	    Address: number;
	    Config: project.MutationConfig;
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
	export interface DataSource {
	    Type: string;
	    Value?: number;
	    Min?: number;
	    Max?: number;
	    Amplitude?: number;
	    Frequency?: number;
	    Offset?: number;
	    Phase?: number;
	    PeriodMS?: number;
	    Start?: number;
	    Step?: number;
	    Wrap?: boolean;
	    Values?: number[];
	    LoopPlayback?: boolean;
	}
	export interface DataSourceConfig {
	    Source: DataSource;
	    UpdateIntervalMS?: number;
	}
	export interface MutationConfig {
	    Enabled: boolean;
	    Mode: string;
	    PeriodMS?: number;
	    Step: number;
	    Min: number;
	    Max: number;
	}
	export interface Point {
	    Address: number;
	    RegisterType: string;
	    DataType: string;
	    Endian?: string;
	    Name?: string;
	    Comment?: string;
	    Mutation?: MutationConfig;
	    DataSource?: DataSourceConfig;
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
	export interface Transport {
	    Type: string;
	    Host?: string;
	    Port?: number;
	    Serial?: SerialConfig;
	    TLS?: ClientTLS;
	    ServerTLS?: ServerTLS;
	}

}
