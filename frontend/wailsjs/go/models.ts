export namespace links {
	
	export class Resolution {
	    kind: string;
	    path: string;
	    fragment: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Resolution(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.fragment = source["fragment"];
	        this.url = source["url"];
	    }
	}

}

export namespace main {
	
	export class PrewarmState {
	    supported: boolean;
	    enabled: boolean;
	    needsApproval: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PrewarmState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.enabled = source["enabled"];
	        this.needsApproval = source["needsApproval"];
	    }
	}

}

export namespace render {
	
	export class Doc {
	    html: string;
	    title: string;
	    path: string;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new Doc(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.html = source["html"];
	        this.title = source["title"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	    }
	}

}

export namespace settings {
	
	export class Settings {
	    theme: string;
	    fontFamily: string;
	    fontSize: number;
	    contentWidth: number;
	    prewarmAsked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.fontFamily = source["fontFamily"];
	        this.fontSize = source["fontSize"];
	        this.contentWidth = source["contentWidth"];
	        this.prewarmAsked = source["prewarmAsked"];
	    }
	}

}

