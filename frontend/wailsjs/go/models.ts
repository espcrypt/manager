export namespace esp {
	
	export class PortInfo {
	    name: string;
	    vid: string;
	    pid: string;
	    serial: string;
	    product: string;
	    kind: string;
	    likely: boolean;
	    usbJtag: boolean;
	    passkey: boolean;
	    vault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PortInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.vid = source["vid"];
	        this.pid = source["pid"];
	        this.serial = source["serial"];
	        this.product = source["product"];
	        this.kind = source["kind"];
	        this.likely = source["likely"];
	        this.usbJtag = source["usbJtag"];
	        this.passkey = source["passkey"];
	        this.vault = source["vault"];
	    }
	}

}

export namespace espcrypt {
	
	export class DataObject {
	    id: number;
	    fid: string;
	    label: string;
	    app: string;
	    private: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new DataObject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.fid = source["fid"];
	        this.label = source["label"];
	        this.app = source["app"];
	        this.private = source["private"];
	        this.size = source["size"];
	    }
	}
	export class PinStatus {
	    verified: boolean;
	    retries: number;
	    blocked: boolean;
	    notInitialized: boolean;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new PinStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.verified = source["verified"];
	        this.retries = source["retries"];
	        this.blocked = source["blocked"];
	        this.notInitialized = source["notInitialized"];
	        this.text = source["text"];
	    }
	}
	export class RadioClient {
	    kid: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new RadioClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kid = source["kid"];
	        this.label = source["label"];
	    }
	}
	export class RadioStatus {
	    name: string;
	    features: Record<string, boolean>;
	    ssid: string;
	    wifiConnected: boolean;
	    ip: string;
	    port: number;
	    bleConnected: boolean;
	    passkeySupported: boolean;
	    safeMode: boolean;
	    clients?: RadioClient[];
	
	    static createFrom(source: any = {}) {
	        return new RadioStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.features = source["features"];
	        this.ssid = source["ssid"];
	        this.wifiConnected = source["wifiConnected"];
	        this.ip = source["ip"];
	        this.port = source["port"];
	        this.bleConnected = source["bleConnected"];
	        this.passkeySupported = source["passkeySupported"];
	        this.safeMode = source["safeMode"];
	        this.clients = this.convertValues(source["clients"], RadioClient);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Storage {
	    usedEntries: number;
	    freeEntries: number;
	    totalEntries: number;
	    entryBytes: number;
	    partitionBytes: number;
	    containerBytes: number;
	    maxContainerBytes: number;
	    maxObjects: number;
	    maxObjectBytes: number;
	    objects: number;
	
	    static createFrom(source: any = {}) {
	        return new Storage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.usedEntries = source["usedEntries"];
	        this.freeEntries = source["freeEntries"];
	        this.totalEntries = source["totalEntries"];
	        this.entryBytes = source["entryBytes"];
	        this.partitionBytes = source["partitionBytes"];
	        this.containerBytes = source["containerBytes"];
	        this.maxContainerBytes = source["maxContainerBytes"];
	        this.maxObjects = source["maxObjects"];
	        this.maxObjectBytes = source["maxObjectBytes"];
	        this.objects = source["objects"];
	    }
	}

}

export namespace main {
	
	export class AgentStatus {
	    running: boolean;
	    socket: string;
	    configPath: string;
	    configInstalled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.socket = source["socket"];
	        this.configPath = source["configPath"];
	        this.configInstalled = source["configInstalled"];
	    }
	}
	export class BundledFirmware {
	    firmware: string;
	    chip: string;
	    version: string;
	    built: string;
	    size: number;
	    release: string;
	
	    static createFrom(source: any = {}) {
	        return new BundledFirmware(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.firmware = source["firmware"];
	        this.chip = source["chip"];
	        this.version = source["version"];
	        this.built = source["built"];
	        this.size = source["size"];
	        this.release = source["release"];
	    }
	}
	export class ChipInfo {
	    chip: string;
	    mac: string;
	    flashMb: number;
	    flashId: string;
	    securityInfoAvailable: boolean;
	    secureBoot: boolean;
	    flashEncrypted: boolean;
	    secureDownload: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChipInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chip = source["chip"];
	        this.mac = source["mac"];
	        this.flashMb = source["flashMb"];
	        this.flashId = source["flashId"];
	        this.securityInfoAvailable = source["securityInfoAvailable"];
	        this.secureBoot = source["secureBoot"];
	        this.flashEncrypted = source["flashEncrypted"];
	        this.secureDownload = source["secureDownload"];
	    }
	}
	export class ESPOptions {
	    port: string;
	    reset: string;
	    baud: number;
	
	    static createFrom(source: any = {}) {
	        return new ESPOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.reset = source["reset"];
	        this.baud = source["baud"];
	    }
	}
	export class FlashRequest {
	    port: string;
	    reset: string;
	    baud: number;
	    firmware: string;
	    file: string;
	    keepData: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FlashRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.reset = source["reset"];
	        this.baud = source["baud"];
	        this.firmware = source["firmware"];
	        this.file = source["file"];
	        this.keepData = source["keepData"];
	    }
	}
	export class LogEntry {
	    time: string;
	    level: string;
	    msg: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.level = source["level"];
	        this.msg = source["msg"];
	    }
	}
	export class MirrorBoard {
	    id: string;
	    label: string;
	    connected: boolean;
	    unlocked: boolean;
	    rev: number;
	    passkeyRev: number;
	    passkey: boolean;
	    transport: string;
	    hash: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorBoard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.connected = source["connected"];
	        this.unlocked = source["unlocked"];
	        this.rev = source["rev"];
	        this.passkeyRev = source["passkeyRev"];
	        this.passkey = source["passkey"];
	        this.transport = source["transport"];
	        this.hash = source["hash"];
	    }
	}
	export class MirrorConfig {
	    main: string;
	    backup: string;
	    rev: number;
	    passkeyRev: number;
	    hash: string;
	    passkeysPending: boolean;
	    passkeyFrom: string;
	    lastSync: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.main = source["main"];
	        this.backup = source["backup"];
	        this.rev = source["rev"];
	        this.passkeyRev = source["passkeyRev"];
	        this.hash = source["hash"];
	        this.passkeysPending = source["passkeysPending"];
	        this.passkeyFrom = source["passkeyFrom"];
	        this.lastSync = source["lastSync"];
	    }
	}
	export class MirrorStatus {
	    configured: boolean;
	    main: MirrorBoard;
	    backup: MirrorBoard;
	    state: string;
	    message: string;
	    lastSync: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.main = this.convertValues(source["main"], MirrorBoard);
	        this.backup = this.convertValues(source["backup"], MirrorBoard);
	        this.state = source["state"];
	        this.message = source["message"];
	        this.lastSync = source["lastSync"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PasskeyDeviceInfo {
	    port: string;
	    status: passkey.Status;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PasskeyDeviceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.status = this.convertValues(source["status"], passkey.Status);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SSHKeyView {
	    id: number;
	    label: string;
	    type: string;
	    fingerprint: string;
	    authorized: string;
	    deviceId: string;
	    deviceLabel: string;
	    second: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SSHKeyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.fingerprint = source["fingerprint"];
	        this.authorized = source["authorized"];
	        this.deviceId = source["deviceId"];
	        this.deviceLabel = source["deviceLabel"];
	        this.second = source["second"];
	    }
	}
	export class WirelessPairing {
	    kid: string;
	    key: string;
	    id: string;
	    name: string;
	    chip: string;
	    lastIp?: string;
	    wifi: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WirelessPairing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kid = source["kid"];
	        this.key = source["key"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.chip = source["chip"];
	        this.lastIp = source["lastIp"];
	        this.wifi = source["wifi"];
	    }
	}
	export class Settings {
	    rememberPin: boolean;
	    confirmSign: boolean;
	    agentAutostart: boolean;
	    autoConnect: boolean;
	    lastReader: string;
	    activeVaultDevice: string;
	    keyMode: number;
	    secondVaultDevice: string;
	    mountBase: string;
	    vaults?: vault.Vault[];
	    lockOnRemove: boolean;
	    openAfterUnlock: boolean;
	    notifyOnConnect: boolean;
	    runInBackground: boolean;
	    noAutoUpdate: boolean;
	    mirror: MirrorConfig;
	    sshKeys?: Record<string, Array<cachedSSHKey>>;
	    wireless?: Record<string, WirelessPairing>;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rememberPin = source["rememberPin"];
	        this.confirmSign = source["confirmSign"];
	        this.agentAutostart = source["agentAutostart"];
	        this.autoConnect = source["autoConnect"];
	        this.lastReader = source["lastReader"];
	        this.activeVaultDevice = source["activeVaultDevice"];
	        this.keyMode = source["keyMode"];
	        this.secondVaultDevice = source["secondVaultDevice"];
	        this.mountBase = source["mountBase"];
	        this.vaults = this.convertValues(source["vaults"], vault.Vault);
	        this.lockOnRemove = source["lockOnRemove"];
	        this.openAfterUnlock = source["openAfterUnlock"];
	        this.notifyOnConnect = source["notifyOnConnect"];
	        this.runInBackground = source["runInBackground"];
	        this.noAutoUpdate = source["noAutoUpdate"];
	        this.mirror = this.convertValues(source["mirror"], MirrorConfig);
	        this.sshKeys = this.convertValues(source["sshKeys"], Array<cachedSSHKey>, true);
	        this.wireless = this.convertValues(source["wireless"], WirelessPairing, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Status {
	    connected: boolean;
	    deviceType: string;
	    secureBoot: boolean;
	    flashEncrypted: boolean;
	    vaultStorageReady: boolean;
	    port: string;
	    serial: string;
	    pin: espcrypt.PinStatus;
	    pinCached: boolean;
	    agent: AgentStatus;
	    passkey: boolean;
	    storage: espcrypt.Storage;
	    firmwareVersion: string;
	    firmwareBuilt: string;
	    transport: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.deviceType = source["deviceType"];
	        this.secureBoot = source["secureBoot"];
	        this.flashEncrypted = source["flashEncrypted"];
	        this.vaultStorageReady = source["vaultStorageReady"];
	        this.port = source["port"];
	        this.serial = source["serial"];
	        this.pin = this.convertValues(source["pin"], espcrypt.PinStatus);
	        this.pinCached = source["pinCached"];
	        this.agent = this.convertValues(source["agent"], AgentStatus);
	        this.passkey = source["passkey"];
	        this.storage = this.convertValues(source["storage"], espcrypt.Storage);
	        this.firmwareVersion = source["firmwareVersion"];
	        this.firmwareBuilt = source["firmwareBuilt"];
	        this.transport = source["transport"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    available: boolean;
	    notes: string;
	    url: string;
	    asset: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.notes = source["notes"];
	        this.url = source["url"];
	        this.asset = source["asset"];
	    }
	}
	export class VaultDeviceInfo {
	    id: string;
	    label: string;
	    port: string;
	    chip: string;
	    serial: string;
	    active: boolean;
	    second: boolean;
	    unlocked: boolean;
	    key: string;
	    transport: string;
	
	    static createFrom(source: any = {}) {
	        return new VaultDeviceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.port = source["port"];
	        this.chip = source["chip"];
	        this.serial = source["serial"];
	        this.active = source["active"];
	        this.second = source["second"];
	        this.unlocked = source["unlocked"];
	        this.key = source["key"];
	        this.transport = source["transport"];
	    }
	}
	export class WirelessConfig {
	    features: Record<string, boolean>;
	    ssid?: string;
	    password?: string;
	    newName: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WirelessConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.features = source["features"];
	        this.ssid = source["ssid"];
	        this.password = source["password"];
	        this.newName = source["newName"];
	    }
	}
	export class WirelessInfo {
	    deviceId: string;
	    label: string;
	    transport: string;
	    supported: boolean;
	    radio?: espcrypt.RadioStatus;
	    paired: boolean;
	    pairedAs: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new WirelessInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.label = source["label"];
	        this.transport = source["transport"];
	        this.supported = source["supported"];
	        this.radio = this.convertValues(source["radio"], espcrypt.RadioStatus);
	        this.paired = source["paired"];
	        this.pairedAs = source["pairedAs"];
	        this.name = source["name"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class cachedSSHKey {
	    id: number;
	    label: string;
	    pub: string;
	
	    static createFrom(source: any = {}) {
	        return new cachedSSHKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.pub = source["pub"];
	    }
	}

}

export namespace passkey {
	
	export class Credential {
	    id: string;
	    rpId: string;
	    userName: string;
	    displayName: string;
	    created: number;
	
	    static createFrom(source: any = {}) {
	        return new Credential(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rpId = source["rpId"];
	        this.userName = source["userName"];
	        this.displayName = source["displayName"];
	        this.created = source["created"];
	    }
	}
	export class Storage {
	    usedEntries: number;
	    freeEntries: number;
	    totalEntries: number;
	    entryBytes: number;
	    partitionBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Storage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.usedEntries = source["usedEntries"];
	        this.freeEntries = source["freeEntries"];
	        this.totalEntries = source["totalEntries"];
	        this.entryBytes = source["entryBytes"];
	        this.partitionBytes = source["partitionBytes"];
	    }
	}
	export class Status {
	    firmware: string;
	    version: string;
	    chip: string;
	    deviceId: string;
	    pinSet: boolean;
	    pinRetries: number;
	    credentials: number;
	    maxCredentials: number;
	    touch: string;
	    rgbLed: boolean;
	    credentialBytes: number;
	    storage: Storage;
	    combined: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.firmware = source["firmware"];
	        this.version = source["version"];
	        this.chip = source["chip"];
	        this.deviceId = source["deviceId"];
	        this.pinSet = source["pinSet"];
	        this.pinRetries = source["pinRetries"];
	        this.credentials = source["credentials"];
	        this.maxCredentials = source["maxCredentials"];
	        this.touch = source["touch"];
	        this.rgbLed = source["rgbLed"];
	        this.credentialBytes = source["credentialBytes"];
	        this.storage = this.convertValues(source["storage"], Storage);
	        this.combined = source["combined"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace vault {
	
	export class CreateRequest {
	    name: string;
	    path: string;
	    sizeMb: number;
	    password: string;
	    encryption: string;
	    hash: string;
	    filesystem: string;
	    pim: number;
	    fullFormat: boolean;
	    secondDevice: string;
	    keyCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.sizeMb = source["sizeMb"];
	        this.password = source["password"];
	        this.encryption = source["encryption"];
	        this.hash = source["hash"];
	        this.filesystem = source["filesystem"];
	        this.pim = source["pim"];
	        this.fullFormat = source["fullFormat"];
	        this.secondDevice = source["secondDevice"];
	        this.keyCount = source["keyCount"];
	    }
	}
	export class KeyfileChoice {
	    id: number;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new KeyfileChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	    }
	}
	export class View {
	    id: string;
	    name: string;
	    path: string;
	    keyfileId: number;
	    keyfileFid: string;
	    hasPassword: boolean;
	    created: string;
	    deviceSerial: string;
	    hash?: string;
	    secondDeviceSerial?: string;
	    secondKeyfileId?: number;
	    secondKeyfileFid?: string;
	    letter?: string;
	    mountDir?: string;
	    exists: boolean;
	    mounted: boolean;
	    mountPoint: string;
	    keyfileOnDevice: boolean;
	    size: number;
	    twoKey: boolean;
	    missing: string[];
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.keyfileId = source["keyfileId"];
	        this.keyfileFid = source["keyfileFid"];
	        this.hasPassword = source["hasPassword"];
	        this.created = source["created"];
	        this.deviceSerial = source["deviceSerial"];
	        this.hash = source["hash"];
	        this.secondDeviceSerial = source["secondDeviceSerial"];
	        this.secondKeyfileId = source["secondKeyfileId"];
	        this.secondKeyfileFid = source["secondKeyfileFid"];
	        this.letter = source["letter"];
	        this.mountDir = source["mountDir"];
	        this.exists = source["exists"];
	        this.mounted = source["mounted"];
	        this.mountPoint = source["mountPoint"];
	        this.keyfileOnDevice = source["keyfileOnDevice"];
	        this.size = source["size"];
	        this.twoKey = source["twoKey"];
	        this.missing = source["missing"];
	    }
	}
	export class OpenResult {
	    vault?: View;
	    choices: KeyfileChoice[];
	
	    static createFrom(source: any = {}) {
	        return new OpenResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.vault = this.convertValues(source["vault"], View);
	        this.choices = this.convertValues(source["choices"], KeyfileChoice);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Vault {
	    id: string;
	    name: string;
	    path: string;
	    keyfileId: number;
	    keyfileFid: string;
	    hasPassword: boolean;
	    created: string;
	    deviceSerial: string;
	    hash?: string;
	    secondDeviceSerial?: string;
	    secondKeyfileId?: number;
	    secondKeyfileFid?: string;
	    letter?: string;
	    mountDir?: string;
	
	    static createFrom(source: any = {}) {
	        return new Vault(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.keyfileId = source["keyfileId"];
	        this.keyfileFid = source["keyfileFid"];
	        this.hasPassword = source["hasPassword"];
	        this.created = source["created"];
	        this.deviceSerial = source["deviceSerial"];
	        this.hash = source["hash"];
	        this.secondDeviceSerial = source["secondDeviceSerial"];
	        this.secondKeyfileId = source["secondKeyfileId"];
	        this.secondKeyfileFid = source["secondKeyfileFid"];
	        this.letter = source["letter"];
	        this.mountDir = source["mountDir"];
	    }
	}

}

export namespace veracrypt {
	
	export class Info {
	    installed: boolean;
	    path: string;
	    formatPath: string;
	    version: string;
	    os: string;
	    fuse: string;
	    broken: boolean;
	    brokenReason: string;
	    canChangePassword: boolean;
	    canHidden: boolean;
	    canCreatePim: boolean;
	    canList: boolean;
	    encryptions: string[];
	    hashes: string[];
	    filesystems: string[];
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.path = source["path"];
	        this.formatPath = source["formatPath"];
	        this.version = source["version"];
	        this.os = source["os"];
	        this.fuse = source["fuse"];
	        this.broken = source["broken"];
	        this.brokenReason = source["brokenReason"];
	        this.canChangePassword = source["canChangePassword"];
	        this.canHidden = source["canHidden"];
	        this.canCreatePim = source["canCreatePim"];
	        this.canList = source["canList"];
	        this.encryptions = source["encryptions"];
	        this.hashes = source["hashes"];
	        this.filesystems = source["filesystems"];
	    }
	}

}

