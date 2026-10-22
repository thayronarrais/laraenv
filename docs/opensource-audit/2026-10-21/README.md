# Revisão de licença e dependências — 2026-10-21

Escopo: desktop LaraEnv e cliente opcional da nuvem, com continuação de publicação dos fontes em 2026-10-22. A preparação inicial partiu de `3ddf5a6`; a continuação de runtimes e dependências partiu de `00b2b71`, o fechamento/publicação partiu de `3268923` e a revisão Redis/Imagick partiu de `8ac1721`. Backend privado, billing, credenciais, produção e dados existentes ficaram fora das alterações. A publicação anterior dos quatro ZIPs pgvector e seus quatro sidecars permanece no release público existente. Os fontes comunitários do desktop foram incorporados ao repositório público LaraEnv, preservando seu histórico público e as releases, sem importar o histórico do builder privado ou incluir o backend.

## Preparação

- A Apache-2.0 existente foi preservada. A qualificação padrão da cláusula de marcas foi restaurada; o RTF agora deriva do LICENSE completo, incluindo o Appendix. Metadados npm declaram Apache-2.0 e continuam `private: true`.
- A confirmação do proprietário sobre redistribuição dos materiais próprios foi registrada em 2026-10-21. Imagens de fornecedores sem procedência exata, artes antigas com marcas incorporadas e mockups foram preservados no arquivo local e excluídos do candidato atual. Os símbolos das integrações são genéricos.
- O Composer PHAR 2.8.5 corresponde ao checksum oficial. Licenças de seus pacotes e recursos aninhados entram no inventário, junto de módulos Go, pacotes npm, Monaco, Codicons e JetBrains Mono.
- O binário TUI Linux antigo, com toolchain anterior e correspondência não demonstrada com os fontes, foi preservado no arquivo local e retirado do candidato. Futuras distribuições devem recompilá-lo.
- A geração de avisos é determinística e tem modo de checagem. O desktop consulta os textos locais sob demanda; o empacotamento inclui os avisos completos.
- Componentes WiX 5.0.2 do MSI mantêm MS-RL e recebem seu código-fonte correspondente junto ao pacote. O build usa explicitamente a estratégia WebView2 `browser`, alinhada ao grafo inventariado, com runtime Microsoft instalado separadamente.

## Segurança de dependências

Toolchain Go **1.26.10**; x/crypto **0.57.0**, x/net **0.60.0**, x/sys **0.48.0**, x/text **0.42.0**, x/sync **0.23.0** e x/term **0.46.0**. As versões foram alinhadas ao grafo de requisitos, além dos mínimos individuais de correção. [Histórico oficial Go](https://go.dev/doc/devel/release), [GO-2026-6809](https://vuln.go.dev/ID/GO-2026-6809.json).

| Verificação atual | Resultado |
| --- | --- |
| Go desktop, tags de produção Windows amd64 | 0 achados em símbolos chamados; 0 em pacotes importados; 1 somente no módulo |
| Go exports SSH, complemento para reflexão Wails | 0 em símbolos; 0 em pacotes; 1 somente no módulo |
| npm frontend, completo e somente produção | 0 achados após migração Tailwind 4 |
| npm screenshots | 0 achados |
| Grafo completo de versões Go, incluindo ferramentas/build | 1 ID: OpenPGP sem versão corrigida; antes eram 27 |
| CLI Wails reconstruído com o grafo do projeto | 0 em símbolos; 0 em pacotes; 1 somente no módulo |

Os scans foram repetidos em **2026-10-22**. Go usa govulncheck 1.9.0 e banco oficial modificado em **2026-10-21T00:27:29Z**. npm consultou o registro oficial em 2026-10-22, após a correção adicional descrita abaixo. A análise estática não prova exploração nem cobre toda chamada dinâmica. A varredura complementar de SSH evita concluir segurança apenas pelo entrypoint Wails.

Frontend fixa Vite **6.4.4**, Vitest **4.1.12**, PostCSS **8.5.23**, Tailwind e seu adaptador PostCSS **4.3.3**; screenshots fixa Vite/Vitest nas mesmas versões. As atualizações npm afetaram versões de desenvolvimento/build/teste; os pacotes de runtime mantiveram suas versões.

A checagem de 2026-10-22 encontrou dois avisos adicionais em `@vitest/mocker`: [GHSA-qxg6-89mg-rqv8](https://github.com/vitest-dev/vitest/security/advisories/GHSA-qxg6-89mg-rqv8) e [GHSA-xp26-3454-mjgp](https://github.com/vitest-dev/vitest/security/advisories/GHSA-xp26-3454-mjgp). Os dois projetos passaram de Vitest 4.1.11 para **4.1.12**, com os oito pacotes correspondentes de desenvolvimento em cada lockfile. As novas auditorias completas de frontend/screenshots e de produção do frontend retornaram zero achados. Essa correção não altera as versões de produção ou o grafo Go.

As cadeias vulneráveis `braces` e `postcss-selector-parser` do Tailwind 3 saíram do lockfile pela migração, sem overrides incompatíveis. A compatibilidade visual foi conferida em 24 pares de páginas compiladas, com três temas e duas larguras; tipografia, geometria e espaçamentos foram preservados. Uma regressão real na altura de linha dos indicadores foi corrigida. O mínimo do WebView2 passa a Chromium 111+, conforme o [suporte oficial do Tailwind 4](https://tailwindcss.com/docs/upgrade-guide).

Foram corrigidas as versões selecionadas das ferramentas: circl **1.6.3**, go-billy **5.9.0**, go-git **5.19.2**, Echo **4.15.3**, goldmark **1.7.17** e x/image **0.45.0**, mantendo Wails **2.12.0**. O build usa a CLI com o grafo deste projeto e `-nosyncgomod`; uma instalação global via `@version` usaria outro grafo. Os requisitos indiretos de segurança das ferramentas devem ser preservados e reavaliados ao executar `go mod tidy`.

O achado Go remanescente é [**GO-2026-5932**](https://pkg.go.dev/vuln/GO-2026-5932), em `x/crypto/openpgp`, sem versão corrigida; os pacotes afetados não aparecem nos caminhos examinados do desktop, dos exports SSH ou da CLI. Isso não equivale a declarar todo o grafo sem avisos.

## Runtimes e retenção de avisos

A [matriz de runtimes](runtime-licenses.md) cobre 40 fontes do catálogo, 41 combinações PECL e os pré-requisitos Microsoft. A primeira revisão inspecionou 25 ZIPs públicos como dados; as continuações acrescentaram arquivos PHP exatos, Cmder full, fontes/SDKs, verificações de checksums e termos Redis/Imagick. O inventário gerado contém **1.010 componentes, 470 textos exatos únicos e um arquivo de fontes WiX**, incluindo seis textos upstream gerais, 100 suplementos Redis e 77 suplementos PECL: sete MongoDB, um memcached e 69 Imagick.

A instalação PECL conserva documentos legais aninhados, hashes e caminhos relativos por extensão/SHA256 do ZIP. As fixtures verificam bytes exatos, idempotência, preservação de documentos existentes e rejeição de entradas inseguras antes das DLLs. Os suplementos novos usam manifests separados; nenhum manifest anterior ou configuração local precisa ser reescrito.

A [revisão Redis](redis-license-closure.md) identificou **dlfcn-win32 LGPL-2.1 ou posterior**, SipHash CC0, os módulos Lua e as atribuições Windows. O core continua BSD-3-Clause; a descrição do conjunto agora é composta. Cem textos exatos, com 194 intervalos de origem, são vinculados ao SHA256 do ZIP 5.0.14.1 e retidos localmente. A rota de fonte da seção 6(a) foi preparada com os oito projetos e dependências identificados; uma futura redistribuição própria terá de entregar esse material efetivamente. Não há exigência deduzida de reproduzir o build bit a bit ou obter os objetos originais quando a fonte utilizável satisfaz a rota escolhida.

A [revisão Imagick](imagick-license-closure.md) acrescenta 69 textos completos, incluindo o NOTICE integral do SDK e licenças referenciadas, somente para Imagick 3.7.0/PHP 8.3. As 166 DLLs acompanhantes coincidem com o SDK; a [análise de fonte](imagick-source-route.md) cobre 32 famílias, oito famílias estáticas e a receita histórica Windows. FLIF inclui encoder e exige os termos LGPL correspondentes. Os snapshots históricos recuperados não provam todas as fontes modificadas usadas; persistem lacunas concretas de religação/fontes, X11/Xext e atribuição Adobe em LibRaw antes de um mirror próprio.

Os quatro ZIPs pgvector e os quatro sidecars SHA256 **foram corrigidos e publicados**, após autorização do proprietário, no [release existente](https://github.com/thayronarrais/laraenv/releases/tag/pgvector-0.8.6). Downloads anônimos e revisão independente conferiram todos os oito hashes/URLs e as quatro licenças completas; os 172 membros originais permanecem byte a byte iguais. A omissão pública foi encerrada. Não houve recompilação de DLLs ou afirmação de correspondência reproduzível fonte/binário.

Os dois ZIPs PHP antes indisponíveis foram encontrados nos arquivos oficiais; os URLs foram corrigidos para os mesmos patches, com licenças, avisos de redistribuição e SBOMs examinados. Os suplementos PECL recuperam sete textos de MongoDB/libbson/libmongoc/libmongocrypt e um do SDK memcached. A instalação mantém os avisos e o manifesto originais, usando manifesto suplementar separado para não bloquear instalações já existentes.

O [follow-up do catálogo](runtime-catalog-followup.md) e o [follow-up dos acompanhantes](runtime-companions-followup.md) registram as etapas anteriores. As revisões Redis/Imagick acima encerram a retenção dos avisos recuperados para os hashes examinados no fluxo atual de download do fornecedor. MySQL/Cmder, fontes/religação Imagick, entrega efetiva da rota Redis e outros conjuntos mantêm condições antes de futuro mirror/bundle; ZIPs manuais e Microsoft mantêm termos próprios. Essas condições não recebem liberação genérica nem exigem publicar o backend privado.

## Publicação dos fontes e próximas distribuições

O [repositório comunitário do desktop](https://github.com/thayronarrais/laraenv) incorpora o snapshot revisado sobre o histórico público já existente. O exportador exclui evidências privadas, saídas locais, backend, caches e mídia fora do escopo, conforme a [política de snapshots](../../source-snapshot.md); ele não remove ou requalifica os commits públicos anteriores. As instruções de [desenvolvimento](../../development.md), [contribuição](../../../CONTRIBUTING.md) e [segurança](../../../SECURITY.md), junto à [CI do desktop](https://github.com/thayronarrais/laraenv/actions/workflows/desktop-checks.yml), fazem parte dos fontes.

Em 2026-10-21, três Windows Sandboxes limpos validaram a instalação e desinstalação MSI, upgrade MSI real de 0.5.7 para o candidato de teste e execução/upgrade manual do pacote portátil. A interface abriu com nuvem desativada e os campos/arquivos sintéticos comparados foram preservados. Os pacotes 0.5.8 usados eram exclusivos de validação; esses resultados correspondem ao snapshot anterior aos ajustes de links comunitários e dependências de teste desta publicação. Não estabelecem uma nova release pública ou validação de todos os dados e runtimes possíveis.

Antes das próximas distribuições:

1. Validar o atualizador automático real e os fluxos de instalação de runtimes e uso de bancos de dados do candidato oficial. O upgrade manual portátil não valida o atualizador interno. Repetir os gates de instalação/upgrade quando o candidato ou o empacotamento mudar.
2. Acompanhar OpenPGP sem correção publicada e repetir scans com o grafo e toolchain exatos antes de distribuir.
3. Cumprir o inventário/fontes de acompanhantes **antes de acrescentar qualquer novo runtime a um mirror ou pacote do projeto**. A correção dos quatro pgvector já foi publicada. Windows/WebView2 permanecem pré-requisitos separados. Os sete achados históricos continuam na política, com resolução/disposição atual explícita.

Os resultados iniciais ficam no [registro anterior](verification.md) e na [validação de dependências](runtime-dependency-verification.md). A [validação de publicação](runtime-publication-verification.md) registra o pgvector; a [validação Redis/Imagick](redis-imagick-verification.md) registra a continuação atual. A política de direitos e avisos está em [docs/licensing.md](../../licensing.md). Os JSONs de 2026-10-19 são registros históricos do inventário anterior, não o estado atual das dependências.
