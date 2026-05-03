import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

const resources = {
  pt: {
    translation: {
      "nav": {
        "docs": "Documentação",
        "lang": "EN"
      },
      "hero": {
        "badge": "v0.1.0 Beta Disponível",
        "title1": "O ambiente de desenvolvimento",
        "title2": "definitivo para Windows.",
        "subtitle": "Substitua o Laragon ou Herd por uma ferramenta mais flexível, multi-runtime e visualmente incrível. Construído com Wails, Go e React.",
        "btn_download": "Baixar LaraEnv (MSI)",
        "btn_github": "Ver no GitHub",
        "footer": "Para Windows 10/11 x64. Binário nativo de ~13MB."
      },
      "features": {
        "title": "Poderoso. Leve. Elegante.",
        "subtitle": "Projetado para resolver dores reais de quem desenvolve em PHP, Node ou Python no Windows.",
        "f1_title": "Multi-Runtime",
        "f1_desc": "Mantenha várias versões de PHP e Node em paralelo. Alterne entre elas globalmente ou defina uma versão específica por projeto.",
        "f2_title": "Serviços Integrados",
        "f2_desc": "Nginx, MySQL, PostgreSQL, Redis e Mailpit rodando nativamente no Windows com 1 clique, sem Docker ou WSL.",
        "f3_title": "Terminal Multi-aba",
        "f3_desc": "PowerShell, CMD, Git Bash ou Cmder integrados. O terminal já abre no diretório do projeto com o PATH apontando para as versões corretas de PHP/Node.",
        "f4_title": "Gestão de Projetos e Vhosts",
        "f4_desc": "O LaraEnv monitora sua pasta 'www'. Ao criar um projeto, ele gera automaticamente o vhost do Nginx, edita o arquivo hosts do Windows e pode gerar certificados SSL self-signed. Suporta criação fácil de projetos Laravel, WordPress ou Blank.",
        "f5_title": "Gerenciador SSH",
        "f5_desc": "Gerencie hosts remotos, gere chaves ED25519/RSA e conecte-se com 1 clique usando o terminal integrado."
      },
      "showcase": {
        "title": "Terminal e SSH Integrados",
        "desc": "Esqueça alternar entre janelas. O LaraEnv possui um terminal robusto (xterm.js) conectado via ConPTY. Seu estado é preservado mesmo mudando de página no app.",
        "li1": "Suporte a PowerShell, CMD e Git Bash.",
        "li2": "Sessões persistentes em background.",
        "li3": "Geração e gestão de chaves SSH."
      },
      "footer": {
        "desc": "Inspirado em Laragon e Laravel Herd. Open-source sob a licença MIT.",
        "donate": "Doar (PayPal)"
      }
    }
  },
  en: {
    translation: {
      "nav": {
        "docs": "Documentation",
        "lang": "PT"
      },
      "hero": {
        "badge": "v0.1.0 Beta Available",
        "title1": "The ultimate development",
        "title2": "environment for Windows.",
        "subtitle": "Replace Laragon or Herd with a more flexible, multi-runtime and visually stunning tool. Built with Wails, Go, and React.",
        "btn_download": "Download LaraEnv (MSI)",
        "btn_github": "View on GitHub",
        "footer": "For Windows 10/11 x64. Native binary of ~13MB."
      },
      "features": {
        "title": "Powerful. Lightweight. Elegant.",
        "subtitle": "Designed to solve real pains for those developing in PHP, Node, or Python on Windows.",
        "f1_title": "Multi-Runtime",
        "f1_desc": "Keep multiple versions of PHP and Node in parallel. Switch between them globally or set a specific version per project.",
        "f2_title": "Integrated Services",
        "f2_desc": "Nginx, MySQL, PostgreSQL, Redis, and Mailpit running natively on Windows with 1-click, without Docker or WSL.",
        "f3_title": "Multi-tab Terminal",
        "f3_desc": "Integrated PowerShell, CMD, Git Bash, or Cmder. The terminal opens directly in the project directory with the PATH pointing to the correct PHP/Node versions.",
        "f4_title": "Projects & Vhosts Management",
        "f4_desc": "LaraEnv monitors your 'www' folder. When creating a project, it automatically generates the Nginx vhost, edits the Windows hosts file, and can generate self-signed SSL certs. Supports easy scaffolding for Laravel, WordPress, or Blank projects.",
        "f5_title": "SSH Manager",
        "f5_desc": "Manage remote hosts, generate ED25519/RSA keys, and connect with 1-click using the integrated terminal."
      },
      "showcase": {
        "title": "Integrated Terminal & SSH",
        "desc": "Forget switching between windows. LaraEnv features a robust terminal (xterm.js) connected via ConPTY. Its state is preserved even when switching pages within the app.",
        "li1": "Support for PowerShell, CMD, and Git Bash.",
        "li2": "Persistent background sessions.",
        "li3": "SSH key generation and management."
      },
      "footer": {
        "desc": "Inspired by Laragon and Laravel Herd. Open-source under the MIT license.",
        "donate": "Donate (PayPal)"
      }
    }
  }
};

i18n
  .use(initReactI18next)
  .init({
    resources,
    lng: 'pt',
    fallbackLng: 'en',
    interpolation: {
      escapeValue: false
    }
  });

export default i18n;
