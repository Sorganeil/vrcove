export type EntityKind='avatar'|'world'|'user'|'group';
export const entityPath=(kind:EntityKind,id:string)=>'/'+kind+'/'+encodeURIComponent(id);
export const copyEntityLink=(kind:EntityKind,id:string)=>navigator.clipboard.writeText(window.location.origin+entityPath(kind,id));
export const covePath=(kind:'cove'|'collection',id:string)=>'/'+kind+'/'+encodeURIComponent(id);
export function goTo(path:string){window.history.pushState(null,'',path);window.dispatchEvent(new PopStateEvent('popstate'));}
// A real link to an in-app path: copying or middle-clicking gives the shareable URL, a plain click switches pages without reloading.
export function appLink(path:string){return {href:path,onClick:(e:{metaKey:boolean;ctrlKey:boolean;shiftKey:boolean;altKey:boolean;button:number;preventDefault:()=>void})=>{if(e.metaKey||e.ctrlKey||e.shiftKey||e.altKey||e.button)return;e.preventDefault();goTo(path);}};}
