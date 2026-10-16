import {Languages} from 'lucide-react';
import {languages,setLocale,t,useLocale,type Locale} from './i18n';

export default function LanguagePicker({compact=false}:{compact?:boolean}){
 const locale=useLocale();
 return <label className={'language-picker'+(compact?' compact':'')}>
  <Languages size={16} aria-hidden="true"/>
  <span className={compact?'sr-only':''}>{t('Language')}</span>
  <select aria-label={t('Choose language')} value={locale} onChange={event=>setLocale(event.target.value as Locale)}>
   {languages.map(language=><option key={language.code} value={language.code} lang={language.code}>{language.name}</option>)}
  </select>
 </label>;
}
