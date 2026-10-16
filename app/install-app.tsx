import {useEffect,useState} from 'react';
import {Download,Share,SquarePlus,X} from 'lucide-react';
import {t as tr,useLocale} from './i18n';

type PromptEvent=Event&{prompt:()=>Promise<void>;userChoice:Promise<{outcome:'accepted'|'dismissed'}>};
const standalone=()=>window.matchMedia?.('(display-mode: standalone)').matches||(navigator as {standalone?:boolean}).standalone===true;
// iPhone and iPad browsers have no install prompt; they use Add to Home Screen.
const appleMobile=()=>/iPad|iPhone|iPod/.test(navigator.userAgent)||(navigator.platform==='MacIntel'&&navigator.maxTouchPoints>1);

/* "Install app" button: uses the browser's install prompt, or shows Add to Home Screen steps on iOS. Hidden once installed. */
export default function InstallApp({className=''}:{className?:string}){useLocale();
 const [prompt,setPrompt]=useState<PromptEvent|null>(null),[installed,setInstalled]=useState(standalone),[help,setHelp]=useState(false);
 useEffect(()=>{
  const ready=(e:Event)=>{e.preventDefault();setPrompt(e as PromptEvent);};
  const done=()=>{setInstalled(true);setPrompt(null);};
  window.addEventListener('beforeinstallprompt',ready);window.addEventListener('appinstalled',done);
  return()=>{window.removeEventListener('beforeinstallprompt',ready);window.removeEventListener('appinstalled',done);};
 },[]);
 if(installed||(!prompt&&!appleMobile()))return null;
 async function install(){
  if(!prompt){setHelp(true);return;}
  await prompt.prompt();const {outcome}=await prompt.userChoice;setPrompt(null);if(outcome==='accepted')setInstalled(true);
 }
 return <><button className={'nav-item install-app '+className} onClick={install}><Download size={18}/><span>{tr("Install app")}</span></button>
  {help&&<div className="modal-backdrop" onMouseDown={e=>{if(e.target===e.currentTarget)setHelp(false);}}><div className="modal" role="dialog" aria-modal="true" aria-label={tr("Install VRCove")}><button className="modal-close" aria-label={tr("Close dialog")} onClick={()=>setHelp(false)}><X size={20}/></button><div className="confirm-content install-help"><h2>{tr("Install VRCove")}</h2><ol><li><Share size={17}/>{tr("Tap the Share button in Safari.")}</li><li><SquarePlus size={17}/>{tr("Choose Add to Home Screen.")}</li><li><Download size={17}/>{tr("Tap Add. VRCove opens like an app.")}</li></ol></div></div></div>}</>;
}
