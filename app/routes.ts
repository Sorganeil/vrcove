export type EntityKind='avatar'|'world'|'user'|'group';
export const entityPath=(kind:EntityKind,id:string)=>'/'+kind+'/'+encodeURIComponent(id);
export const copyEntityLink=(kind:EntityKind,id:string)=>navigator.clipboard.writeText(window.location.origin+entityPath(kind,id));
