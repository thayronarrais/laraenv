import { motion } from 'framer-motion';
import { 
  Terminal, Server, Database, Globe, Download, GitBranch, 
  ChevronRight, Layers, ShieldCheck 
} from 'lucide-react';
import { useTranslation } from 'react-i18next';

export default function App() {
  const { t, i18n } = useTranslation();

  const toggleLanguage = () => {
    const nextLang = i18n.language === 'pt' ? 'en' : 'pt';
    i18n.changeLanguage(nextLang);
  };

  return (
    <div className="min-h-screen bg-[#0B0F19] text-slate-300 font-sans selection:bg-violet-500/30">
      {/* Background Orbs */}
      <div className="fixed inset-0 overflow-hidden pointer-events-none z-0">
        <div className="absolute top-[-10%] left-[-10%] w-[40%] h-[40%] rounded-full bg-violet-600/20 blur-[120px]" />
        <div className="absolute top-[20%] right-[-10%] w-[30%] h-[30%] rounded-full bg-blue-600/20 blur-[100px]" />
        <div className="absolute bottom-[-10%] left-[20%] w-[40%] h-[40%] rounded-full bg-indigo-600/10 blur-[120px]" />
      </div>

      <div className="relative z-10">
        {/* Navigation */}
        <nav className="container mx-auto px-6 py-6 flex justify-between items-center">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-violet-500 to-blue-600 flex items-center justify-center shadow-lg shadow-violet-500/20">
              <Layers className="text-white w-5 h-5" />
            </div>
            <span className="text-xl font-bold text-white tracking-tight">LaraEnv</span>
          </div>
          <div className="flex items-center gap-4">
            <button 
              onClick={toggleLanguage}
              className="px-3 py-1.5 rounded-lg bg-violet-500/10 text-violet-400 hover:bg-violet-500/20 transition-colors text-sm font-bold"
            >
              {t('nav.lang')}
            </button>
            <a href="https://github.com/thayronarrais/laraenv" target="_blank" rel="noreferrer" className="text-slate-400 hover:text-white transition-colors">
              <GitBranch className="w-6 h-6" />
            </a>
            <button className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 transition-colors text-sm font-medium">
              {t('nav.docs')}
            </button>
          </div>
        </nav>

        {/* Hero Section */}
        <header className="container mx-auto px-6 pt-24 pb-32 text-center">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.6 }}
            className="max-w-4xl mx-auto"
          >
            <span className="inline-block py-1 px-3 rounded-full bg-violet-500/10 border border-violet-500/20 text-violet-400 text-sm font-medium mb-6">
              {t('hero.badge')}
            </span>
            <h1 className="text-5xl md:text-7xl font-bold text-white mb-8 tracking-tight leading-tight">
              {t('hero.title1')} <br />
              <span className="text-transparent bg-clip-text bg-gradient-to-r from-violet-400 to-blue-500">
                {t('hero.title2')}
              </span>
            </h1>
            <p className="text-lg md:text-xl text-slate-400 mb-10 max-w-2xl mx-auto leading-relaxed">
              {t('hero.subtitle')}
            </p>
            
            <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
              <a
                href="https://github.com/thayronarrais/laraenv/releases/latest/download/LaraEnv-Setup.msi"
                className="flex items-center gap-2 px-8 py-4 rounded-xl bg-gradient-to-r from-violet-600 to-blue-600 text-white font-medium shadow-lg shadow-violet-500/25 hover:shadow-violet-500/40 hover:-translate-y-0.5 transition-all w-full sm:w-auto justify-center"
              >
                <Download className="w-5 h-5" />
                {t('hero.btn_download')}
              </a>
              <a href="https://github.com/thayronarrais/laraenv" target="_blank" rel="noreferrer" className="flex items-center gap-2 px-8 py-4 rounded-xl bg-[#1A233A] border border-white/10 text-white font-medium hover:bg-white/5 transition-all w-full sm:w-auto justify-center">
                <GitBranch className="w-5 h-5" />
                {t('hero.btn_github')}
              </a>
            </div>
            
            <p className="mt-6 text-sm text-slate-500">
              {t('hero.footer')}
            </p>
          </motion.div>
        </header>

        {/* Dashboard Mockup Showcase */}
        <section className="container mx-auto px-6 mb-32">
          <motion.div 
            initial={{ opacity: 0, y: 40 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.8 }}
            className="relative mx-auto max-w-5xl"
          >
            <div className="absolute -inset-1 bg-gradient-to-r from-violet-500 to-blue-600 rounded-2xl blur opacity-30"></div>
            <div className="relative rounded-2xl border border-white/10 bg-[#0B0F19] shadow-2xl overflow-hidden flex items-center justify-center">
              <img 
                src="/dashboard-mockup.png" 
                alt="LaraEnv Dashboard" 
                className="w-full h-auto object-cover"
              />
            </div>
          </motion.div>
        </section>

        {/* Features Grid */}
        <section className="container mx-auto px-6 py-24">
          <div className="text-center mb-16">
            <h2 className="text-3xl md:text-4xl font-bold text-white mb-4">{t('features.title')}</h2>
            <p className="text-slate-400 max-w-2xl mx-auto">
              {t('features.subtitle')}
            </p>
          </div>

          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6 max-w-6xl mx-auto">
            {/* Feature 1 */}
            <div className="glass-card p-8 group hover:-translate-y-1 transition-all duration-300">
              <div className="w-12 h-12 rounded-lg bg-blue-500/10 flex items-center justify-center mb-6 border border-blue-500/20 group-hover:bg-blue-500/20 transition-colors">
                <Server className="w-6 h-6 text-blue-400" />
              </div>
              <h3 className="text-xl font-semibold text-white mb-3">{t('features.f1_title')}</h3>
              <p className="text-slate-400 leading-relaxed text-sm">
                {t('features.f1_desc')}
              </p>
            </div>

            {/* Feature 2 */}
            <div className="glass-card p-8 group hover:-translate-y-1 transition-all duration-300">
              <div className="w-12 h-12 rounded-lg bg-emerald-500/10 flex items-center justify-center mb-6 border border-emerald-500/20 group-hover:bg-emerald-500/20 transition-colors">
                <Database className="w-6 h-6 text-emerald-400" />
              </div>
              <h3 className="text-xl font-semibold text-white mb-3">{t('features.f2_title')}</h3>
              <p className="text-slate-400 leading-relaxed text-sm">
                {t('features.f2_desc')}
              </p>
            </div>

            {/* Feature 3 */}
            <div className="glass-card p-8 group hover:-translate-y-1 transition-all duration-300 lg:col-span-1 md:col-span-2">
              <div className="w-12 h-12 rounded-lg bg-violet-500/10 flex items-center justify-center mb-6 border border-violet-500/20 group-hover:bg-violet-500/20 transition-colors">
                <Terminal className="w-6 h-6 text-violet-400" />
              </div>
              <h3 className="text-xl font-semibold text-white mb-3">{t('features.f3_title')}</h3>
              <p className="text-slate-400 leading-relaxed text-sm">
                {t('features.f3_desc')}
              </p>
            </div>

            {/* Feature 4 */}
            <div className="glass-card p-8 group hover:-translate-y-1 transition-all duration-300 md:col-span-2">
              <div className="w-12 h-12 rounded-lg bg-amber-500/10 flex items-center justify-center mb-6 border border-amber-500/20 group-hover:bg-amber-500/20 transition-colors">
                <Globe className="w-6 h-6 text-amber-400" />
              </div>
              <h3 className="text-xl font-semibold text-white mb-3">{t('features.f4_title')}</h3>
              <p className="text-slate-400 leading-relaxed text-sm">
                {t('features.f4_desc')}
              </p>
            </div>

            {/* Feature 5 */}
            <div className="glass-card p-8 group hover:-translate-y-1 transition-all duration-300">
              <div className="w-12 h-12 rounded-lg bg-pink-500/10 flex items-center justify-center mb-6 border border-pink-500/20 group-hover:bg-pink-500/20 transition-colors">
                <ShieldCheck className="w-6 h-6 text-pink-400" />
              </div>
              <h3 className="text-xl font-semibold text-white mb-3">{t('features.f5_title')}</h3>
              <p className="text-slate-400 leading-relaxed text-sm">
                {t('features.f5_desc')}
              </p>
            </div>
          </div>
        </section>

        {/* Terminal Showcase */}
        <section className="container mx-auto px-6 py-24">
          <div className="flex flex-col lg:flex-row items-center gap-16 max-w-6xl mx-auto">
            <div className="lg:w-1/2">
              <h2 className="text-3xl md:text-4xl font-bold text-white mb-6">
                {t('showcase.title')}
              </h2>
              <p className="text-slate-400 mb-8 text-lg leading-relaxed">
                {t('showcase.desc')}
              </p>
              <ul className="space-y-4 mb-8">
                {[
                  t('showcase.li1'),
                  t('showcase.li2'),
                  t('showcase.li3')
                ].map((item, i) => (
                  <li key={i} className="flex items-center gap-3 text-slate-300">
                    <ChevronRight className="w-5 h-5 text-violet-400" />
                    <span>{item}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div className="lg:w-1/2 w-full">
              <motion.div 
                initial={{ opacity: 0, x: 40 }}
                whileInView={{ opacity: 1, x: 0 }}
                viewport={{ once: true }}
                className="relative rounded-xl border border-white/10 bg-[#0B0F19] shadow-2xl p-2"
              >
                 <img 
                  src="/terminal-mockup.png" 
                  alt="Terminal Mockup" 
                  className="w-full h-auto rounded-lg border border-white/5"
                />
              </motion.div>
            </div>
          </div>
        </section>

        {/* Footer */}
        <footer className="border-t border-white/10 mt-24">
          <div className="container mx-auto px-6 py-12 flex flex-col md:flex-row justify-between items-center gap-6">
            <div className="flex items-center gap-2">
              <Layers className="text-violet-400 w-6 h-6" />
              <span className="text-lg font-bold text-white">LaraEnv</span>
            </div>
            
            <p className="text-slate-500 text-sm text-center">
              {t('footer.desc')}
            </p>

            <div className="flex items-center gap-4">
              <a href="https://github.com/thayronarrais/laraenv" className="text-slate-400 hover:text-white transition-colors">
                GitHub
              </a>
              <a href="https://www.paypal.com/donate/?business=AQNPEX7PVKRHN&no_recurring=0&currency_code=USD" className="text-slate-400 hover:text-white transition-colors">
                {t('footer.donate')}
              </a>
            </div>
          </div>
        </footer>
      </div>
    </div>
  );
}
