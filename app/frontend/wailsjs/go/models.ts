export namespace main {
	
	export class Artifact {
	    id: string;
	    kind: string;
	    title: string;
	    content: string;
	    source_path: string;
	    source_app: string;
	    heat_score: number;
	    tags: string[];
	    due_at?: number;
	
	    static createFrom(source: any = {}) {
	        return new Artifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.source_path = source["source_path"];
	        this.source_app = source["source_app"];
	        this.heat_score = source["heat_score"];
	        this.tags = source["tags"];
	        this.due_at = source["due_at"];
	    }
	}
	export class ArtifactDetail {
	    artifact: Artifact;
	    related: Artifact[];
	
	    static createFrom(source: any = {}) {
	        return new ArtifactDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact = this.convertValues(source["artifact"], Artifact);
	        this.related = this.convertValues(source["related"], Artifact);
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
	export class ComponentStatus {
	    connected: boolean;
	    state: string;
	    uptime_seconds: number;
	    fault?: string;
	
	    static createFrom(source: any = {}) {
	        return new ComponentStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.state = source["state"];
	        this.uptime_seconds = source["uptime_seconds"];
	        this.fault = source["fault"];
	    }
	}
	export class Status {
	    agent: ComponentStatus;
	    native: ComponentStatus;
	    artifact_counts: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = this.convertValues(source["agent"], ComponentStatus);
	        this.native = this.convertValues(source["native"], ComponentStatus);
	        this.artifact_counts = source["artifact_counts"];
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

