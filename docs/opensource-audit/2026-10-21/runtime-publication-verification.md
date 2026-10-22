# Publicação pgvector e fechamento de runtimes — 2026-10-21

Continuação sobre `3268923`, branch `codex/workspace-source-control`. O proprietário autorizou expressamente a publicação dos quatro ZIPs corrigidos e o fechamento das pendências legais dos runtimes. O backend, credenciais, dados operacionais e visibilidade dos repositórios privados ficaram preservados. Evidências locais ignoradas em `build/verification/runtime-publication-20261021/`.

## Publicação verificada

Destino explícito: repositório público `thayronarrais/laraenv`, [release `pgvector-0.8.6`](https://github.com/thayronarrais/laraenv/releases/tag/pgvector-0.8.6), ID `371369603`. O remote local aponta para o builder privado; não foi usado como destino implícito. Foram preservados o JSON original do release e seus oito assets antes da substituição. O preflight repetiu IDs, digests e corpo do release, conferiu backups e candidatos, e recusaria mudanças concorrentes observáveis antes da operação.

Os quatro ZIPs e seus quatro sidecars foram substituídos, preservando nomes/URLs. A nota pública explica a inclusão de LICENSE e a mudança de hashes. O nome, tag, alvo e demais metadados do release foram conferidos como preservados pela revisão independente. O histórico de downloads pertence aos assets antigos; a substituição cria novas identidades de asset, registradas na política.

| ZIP corrigido | SHA256 público verificado |
| --- | --- |
| `pgvector-0.8.6-pg15-win-x64.zip` | `690933153c14d49e570db4773fba94517361e878b70f89c43bde554a6a05d0b1` |
| `pgvector-0.8.6-pg16-win-x64.zip` | `3ba886dcda520a1998d13db6afcfb503e892df1b3da68943526fea1c1e64bb34` |
| `pgvector-0.8.6-pg17-win-x64.zip` | `28e08226af4417c3a77da1df6f72a4314ea98c832db069a0cd3d9e1c8d11b99b` |
| `pgvector-0.8.6-pg18-win-x64.zip` | `f1a3f2adabcdd9c5cb99a338cd2cce94f471c14af0d4d6a80bb0bf25c8591fba` |

Root e revisor fizeram downloads anônimos novos e inspeções independentes dos oito assets. Cada ZIP tem os 43 membros originais intactos e apenas um novo membro: `share/doc/pgvector/LICENSE`, 1104 bytes, SHA256 `6bba9ebeb73e27477463b05e5ef1bf303bccbddb3db9bbc95905d351604d6a87`. Os 172 payloads, os metadados dos membros originais e comentários ZIP conferem; os quatro sidecars correspondem exatamente aos novos hashes e os oito digests da API coincidem com os bytes baixados. A licença foi conferida no [tag upstream v0.8.6](https://github.com/pgvector/pgvector/blob/v0.8.6/LICENSE), commit `8ee86c96f0fd72390f890aa8a336fda6d3ab4c6c`.

Provas locais: `before/`, `release-after-upload.json`, `public-downloads/`, `public-verification.json` e `pgvector-review/public-verification.json`. A política contém o registro publicável de URLs, IDs, versões, hashes originais/corrigidos e sidecars. A publicação encerra a omissão de licença desses quatro hashes; não foi um rebuild e não prova ABI ou reprodução fonte/binário. Nenhum payload foi executado.

## Avisos e arquivos recuperados

Os mesmos patches PHP 8.4.25 e 8.6.0beta3 foram recuperados nos arquivos oficiais, e somente seus dois URLs do catálogo foram corrigidos. Os ZIPs têm licenças, notices de redistribuição e SBOMs. Foram mantidos os 404 anteriores como evidência histórica. O [follow-up do catálogo](runtime-catalog-followup.md) registra os hashes exatos, a declaração do fornecedor nos SBOMs e seus limites.

O [follow-up dos acompanhantes](runtime-companions-followup.md) estabeleceu libbson/libmongoc 1.28.1 e libmongocrypt 1.11.0 no MongoDB 1.20.1/PHP 8.4; 431 arquivos do C driver e 174 do crypt conferem com os fontes versionados. Seus sete textos legais exatos foram preservados. As quatro DLLs memcached examinadas são byte-idênticas ao SDK libmemcached-awesome 1.1.4, do qual foi recuperado o LICENSE exato. Esses oito documentos são embutidos e selecionados **somente por SHA256 do ZIP e extensão**, sem aplicar a prova a outras séries PHP.

O manifesto de origem e os avisos já retidos ficam intactos; os suplementos usam `licenses/pecl/<extensão>/<hash-do-ZIP>/supplemental/`, com manifesto próprio, URLs, hashes e escopo. A primeira implementação tentava alterar o manifesto anterior: o revisor identificou que isso bloquearia uma instalação existente, pois a retenção preserva documentos diferentes. O teste reproduziu a falha antes da correção. A implementação final cria o bundle separado, e a regressão confere instalação anterior, preservação byte a byte, complemento e repetição idempotente.

## Validação local

- Suíte completa do instalador: `go test ./internal/installer -count=1` passou com Go 1.26.10, em diretório LaraEnv temporário isolado. `go vet ./internal/installer` passou.
- **49 testes Node passaram**: 45 de licenças/coleta/PHAR/RTF e quatro da cópia de textos legais. Testes novos rejeitam perda/troca de pares publicados, aliases, identidades duplicadas, drift do registry e bytes legais modificados.
- Gerador e `--check` passaram com **841 componentes, 304 textos exatos e um arquivo de fontes WiX**. A cobertura permanece 40 fontes, 41 builds PECL, dois pré-requisitos e seis avisos gerais; os oito avisos PECL suplementares são componentes adicionais. O registro da publicação e a disposição dos sete achados históricos aparecem no inventário e no texto offline.
- Revisão independente confirmou hashes/escopos dos oito textos, dois URLs PHP e inclusão do registry, helper e arquivos nos digests de entrada. Após corrigir a compatibilidade do manifesto, não encontrou outro defeito concreto no escopo.
- O frontend de produção e o executável Windows de revisão compilaram. A fixture Chromium do leitor offline passou com rede externa bloqueada e comparação dos textos completos. A inspeção estática do executável encontrou LICENSE, NOTICE, avisos e inventário completos incorporados; seus 36 módulos têm as versões inventariadas. O desktop não foi iniciado; o aviso de chunk acima de 500 kB permanece no build frontend.
- A leitura do índice Git conferiu **353 digests** de entradas/saídas legais, sem divergências; nenhum backend, evidência ignorada ou saída operacional entrou no índice. A leitura final da API confirmou os mesmos oito IDs/digests e a nota publicada após as demais validações.

## Disposição dos sete achados históricos

| Achado | Estado atual |
| --- | --- |
| pgvector sem LICENSE público | **Resolvido para os quatro hashes corrigidos publicados e verificados.** |
| PHPs indisponíveis | **Resolvido para os dois patches exatos**, com URLs oficiais arquivados, conteúdo legal e SBOMs. |
| Avisos PECL incompletos | **Recuperação e retenção encerradas para os dois ZIPs comprovados**; outros builds e aprovação do conjunto completo permanecem condicionados à evidência própria. |
| Redis sem avisos no ZIP upstream | Cinco avisos conhecidos são retidos localmente. O follow-up identifica notices adicionais dos módulos Lua compilados; não declarar o pacote inteiro liberado para mirror. |
| Fonte correspondente MySQL/Cmder | Condição de eventual redistribuição própria. O fluxo atual continua sendo download do fornecedor; fontes iniciais Clink/Git e versões concretas foram identificadas, sem alegar completude de todos os componentes. |
| Apache/EDB manuais | Condição do arquivo exato selecionado pelo usuário; páginas móveis e HTML não certificam o ZIP. Nenhum novo binário desses produtos foi hospedado pelo projeto. |
| Amostra não é inventário completo | Limite de evidência retido para futuras distribuições; SBOMs e checksums do fornecedor não certificam automaticamente todos os acompanhantes ou direitos de redistribuição. |

Imagick tem 166 acompanhantes idênticos ao SDK oficial, mas seus termos/fontes/relinking ainda precisam de uma rota completa antes de um mirror/bundle. Python traz OpenSSL DLLs sem o texto correspondente no LICENSE composto examinado; Caddy tem SBOM vinculado ao executável, mas metadados de licença incompletos. Essas condições, as de Microsoft e os builds não examinados permanecem explícitas, sem serem promovidas a uma liberação genérica nem a uma exigência de publicar o backend.

A publicação do código desktop, CI/snapshot público e instalação/upgrade em Windows limpo continuam etapas separadas. Nenhum release desktop, push do código privado, mudança de visibilidade, instalação de runtime ou execução de aplicativo foi realizado nesta etapa.
