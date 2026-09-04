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

export namespace buildinfo {
	
	export class Info {
	    name: string;
	    version: string;
	    url: string;
	    urlLabel: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.url = source["url"];
	        this.urlLabel = source["urlLabel"];
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
	    browserVisible: boolean;
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
	        this.browserVisible = source["browserVisible"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace session {
	
	export class SpeakerRecord {
	    index: number;
	    name: string;
	    talkSeconds: number;
	    questionsSeconds: number;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeakerRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.talkSeconds = source["talkSeconds"];
	        this.questionsSeconds = source["questionsSeconds"];
	        this.status = source["status"];
	    }
	}
	export class State {
	    active: boolean;
	    totalBudgetSeconds: number;
	    usedSeconds: number;
	    remainingSeconds: number;
	    currentIndex: number;
	    speakers: SpeakerRecord[];
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.totalBudgetSeconds = source["totalBudgetSeconds"];
	        this.usedSeconds = source["usedSeconds"];
	        this.remainingSeconds = source["remainingSeconds"];
	        this.currentIndex = source["currentIndex"];
	        this.speakers = this.convertValues(source["speakers"], SpeakerRecord);
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
	export class Template {
	    totalMinutes: number;
	    totalSeconds: number;
	    speakerCount: number;
	    speakerNames: string[];
	    talkMinutes: number;
	    talkSeconds: number;
	    questionsMinutes: number;
	    questionsSeconds: number;
	    useDefaultTalk: boolean;
	    useDefaultQuestions: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalMinutes = source["totalMinutes"];
	        this.totalSeconds = source["totalSeconds"];
	        this.speakerCount = source["speakerCount"];
	        this.speakerNames = source["speakerNames"];
	        this.talkMinutes = source["talkMinutes"];
	        this.talkSeconds = source["talkSeconds"];
	        this.questionsMinutes = source["questionsMinutes"];
	        this.questionsSeconds = source["questionsSeconds"];
	        this.useDefaultTalk = source["useDefaultTalk"];
	        this.useDefaultQuestions = source["useDefaultQuestions"];
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
	    reminderSoundId: string;
	    questionsSoundId: string;
	    nextSoundId: string;
	    deviceId: string;
	    volume: number;
	    muteConferenceSound: boolean;
	    sessionTotalMinutes: number;
	    sessionTotalSeconds: number;
	    sessionSpeakerCount: number;
	    sessionSpeakerNames: string[];
	    sessionTalkMinutes: number;
	    sessionTalkSeconds: number;
	    sessionQuestionsMinutes: number;
	    sessionQuestionsSeconds: number;
	    sessionUseDefaultTalk: boolean;
	    sessionUseDefaultQuestions: boolean;
	
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
	        this.reminderSoundId = source["reminderSoundId"];
	        this.questionsSoundId = source["questionsSoundId"];
	        this.nextSoundId = source["nextSoundId"];
	        this.deviceId = source["deviceId"];
	        this.volume = source["volume"];
	        this.muteConferenceSound = source["muteConferenceSound"];
	        this.sessionTotalMinutes = source["sessionTotalMinutes"];
	        this.sessionTotalSeconds = source["sessionTotalSeconds"];
	        this.sessionSpeakerCount = source["sessionSpeakerCount"];
	        this.sessionSpeakerNames = source["sessionSpeakerNames"];
	        this.sessionTalkMinutes = source["sessionTalkMinutes"];
	        this.sessionTalkSeconds = source["sessionTalkSeconds"];
	        this.sessionQuestionsMinutes = source["sessionQuestionsMinutes"];
	        this.sessionQuestionsSeconds = source["sessionQuestionsSeconds"];
	        this.sessionUseDefaultTalk = source["sessionUseDefaultTalk"];
	        this.sessionUseDefaultQuestions = source["sessionUseDefaultQuestions"];
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

