export namespace audio {
	
	export class Device {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class Sound {
	    id: string;
	    label: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new Sound(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.source = source["source"];
	    }
	}

}

export namespace conference {
	
	export class Platform {
	    id: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Platform(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	    }
	}
	export class State {
	    phase: string;
	    platform: string;
	    displayUrl: string;
	    message: string;
	    tested: boolean;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.platform = source["platform"];
	        this.displayUrl = source["displayUrl"];
	        this.message = source["message"];
	        this.tested = source["tested"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace settings {
	
	export class Settings {
	    talkMinutes: number;
	    talkSeconds: number;
	    questionsMinutes: number;
	    questionsSeconds: number;
	    reminderMinutes: number;
	    reminderSeconds: number;
	    soundId: string;
	    questionsSoundId: string;
	    nextSoundId: string;
	    deviceId: string;
	    volume: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.talkMinutes = source["talkMinutes"];
	        this.talkSeconds = source["talkSeconds"];
	        this.questionsMinutes = source["questionsMinutes"];
	        this.questionsSeconds = source["questionsSeconds"];
	        this.reminderMinutes = source["reminderMinutes"];
	        this.reminderSeconds = source["reminderSeconds"];
	        this.soundId = source["soundId"];
	        this.questionsSoundId = source["questionsSoundId"];
	        this.nextSoundId = source["nextSoundId"];
	        this.deviceId = source["deviceId"];
	        this.volume = source["volume"];
	    }
	}

}

export namespace timer {
	
	export class Snapshot {
	    phase: string;
	    isRunning: boolean;
	    isPaused: boolean;
	    remainingSeconds: number;
	    overtimeSeconds: number;
	    talkSeconds: number;
	    questionsSeconds: number;
	    nextReminderIn: number;
	    alertActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.isRunning = source["isRunning"];
	        this.isPaused = source["isPaused"];
	        this.remainingSeconds = source["remainingSeconds"];
	        this.overtimeSeconds = source["overtimeSeconds"];
	        this.talkSeconds = source["talkSeconds"];
	        this.questionsSeconds = source["questionsSeconds"];
	        this.nextReminderIn = source["nextReminderIn"];
	        this.alertActive = source["alertActive"];
	    }
	}

}

