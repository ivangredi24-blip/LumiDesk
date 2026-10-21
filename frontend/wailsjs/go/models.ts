export namespace control {
	
	export class State {
	    connected: boolean;
	    connecting: boolean;
	    scanning: boolean;
	    deviceName: string;
	    deviceAddress: string;
	    devices: domain.Device[];
	    connectedDevices: domain.Device[];
	    selectedDevices: string[];
	    syncDevices: boolean;
	    color: domain.Color;
	    brightness: number;
	    powered: boolean;
	    lastError: string;
	    // Go type: time
	    lastCommandAt: any;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.connecting = source["connecting"];
	        this.scanning = source["scanning"];
	        this.deviceName = source["deviceName"];
	        this.deviceAddress = source["deviceAddress"];
	        this.devices = this.convertValues(source["devices"], domain.Device);
	        this.connectedDevices = this.convertValues(source["connectedDevices"], domain.Device);
	        this.selectedDevices = source["selectedDevices"];
	        this.syncDevices = source["syncDevices"];
	        this.color = this.convertValues(source["color"], domain.Color);
	        this.brightness = source["brightness"];
	        this.powered = source["powered"];
	        this.lastError = source["lastError"];
	        this.lastCommandAt = this.convertValues(source["lastCommandAt"], null);
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

export namespace domain {
	
	export class Color {
	    r: number;
	    g: number;
	    b: number;
	
	    static createFrom(source: any = {}) {
	        return new Color(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.r = source["r"];
	        this.g = source["g"];
	        this.b = source["b"];
	    }
	}
	export class Device {
	    name: string;
	    address: string;
	    rssi: number;
	    connected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.address = source["address"];
	        this.rssi = source["rssi"];
	        this.connected = source["connected"];
	    }
	}
	export class Preset {
	    id: string;
	    name: string;
	    color: Color;
	    hex: string;
	    brightness: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    favorite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.color = this.convertValues(source["color"], Color);
	        this.hex = source["hex"];
	        this.brightness = source["brightness"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.favorite = source["favorite"];
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
	export class Settings {
	    preferredDevice: Device;
	    savedDevices: Device[];
	    selectedDevices: string[];
	    syncDevices: boolean;
	    lastColor: Color;
	    lastBrightness: number;
	    lastPowerOn: boolean;
	    launchAtLogin: boolean;
	    startupLightsOn: boolean;
	    shutdownLightsOff: boolean;
	    restoreLastColor: boolean;
	    startupColorMode: string;
	    startupPresetId: string;
	    favoritePresetId: string;
	    autoReconnect: boolean;
	    minimizeToTray: boolean;
	    closeToTray: boolean;
	    turnOffOnAppExit: boolean;
	    windowPositionSet: boolean;
	    windowX: number;
	    windowY: number;
	    windowWidth: number;
	    windowHeight: number;
	    theme: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preferredDevice = this.convertValues(source["preferredDevice"], Device);
	        this.savedDevices = this.convertValues(source["savedDevices"], Device);
	        this.selectedDevices = source["selectedDevices"];
	        this.syncDevices = source["syncDevices"];
	        this.lastColor = this.convertValues(source["lastColor"], Color);
	        this.lastBrightness = source["lastBrightness"];
	        this.lastPowerOn = source["lastPowerOn"];
	        this.launchAtLogin = source["launchAtLogin"];
	        this.startupLightsOn = source["startupLightsOn"];
	        this.shutdownLightsOff = source["shutdownLightsOff"];
	        this.restoreLastColor = source["restoreLastColor"];
	        this.startupColorMode = source["startupColorMode"];
	        this.startupPresetId = source["startupPresetId"];
	        this.favoritePresetId = source["favoritePresetId"];
	        this.autoReconnect = source["autoReconnect"];
	        this.minimizeToTray = source["minimizeToTray"];
	        this.closeToTray = source["closeToTray"];
	        this.turnOffOnAppExit = source["turnOffOnAppExit"];
	        this.windowPositionSet = source["windowPositionSet"];
	        this.windowX = source["windowX"];
	        this.windowY = source["windowY"];
	        this.windowWidth = source["windowWidth"];
	        this.windowHeight = source["windowHeight"];
	        this.theme = source["theme"];
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

export namespace main {
	
	export class Snapshot {
	    state: control.State;
	    settings: domain.Settings;
	    presets: domain.Preset[];
	    startupAvailable: boolean;
	    recoveryWarnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = this.convertValues(source["state"], control.State);
	        this.settings = this.convertValues(source["settings"], domain.Settings);
	        this.presets = this.convertValues(source["presets"], domain.Preset);
	        this.startupAvailable = source["startupAvailable"];
	        this.recoveryWarnings = source["recoveryWarnings"];
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

