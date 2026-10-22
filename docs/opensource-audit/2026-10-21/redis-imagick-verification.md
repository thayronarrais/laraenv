# Redis e Imagick — integração e validação do desktop

Continuação sobre `8ac1721`, em 2026-10-21. O desktop mantém o backend privado e a nuvem opcional. Foram tratados somente código de retenção de avisos, textos legais e documentação; versões/URLs dos runtimes, contas, planos e dados operacionais não mudaram. Nenhum runtime baixado foi executado, nenhum serviço real foi acessado e nenhum Redis/Imagick foi publicado ou acrescentado ao pacote do desktop.

## Mudança entregue

Redis Windows 5.0.14.1 recebe **100 textos exatos adicionais** para o ZIP SHA256 `018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf`. O registro usa kind, versão, URL e hash do arquivo; a extração e o cálculo SHA256 usam o mesmo arquivo aberto. Os cinco textos antigos e seu manifest ficam intactos. O suplemento tem seu próprio manifest em `licenses/laraenv/redis/5.0.14.1/supplemental/<bundle-id>/`.

Imagick 3.7.0/PHP 8.3 recebe **69 textos exatos adicionais** apenas para SHA256 `4204071decbb06dce9f212a63ace0aa0c1b48fd344176b8274cd7d32ab674c71`. Os quatro documentos legais originais do ZIP são preservados pelo fluxo PECL existente; o novo conteúdo usa o manifest suplementar separado. Os sete avisos MongoDB, o aviso memcached e seus dois registros anteriores continuam idênticos ao baseline.

Os dois registros e todos os textos entram nos hashes de entrada do gerador. `sourceEvidence` conserva hashes, origens/intervalos de comentários ou limites de seleção histórica; esse metadado é uma revisão humana vinculada pelo manifest, não uma certificação automática de fonte/binário. O agregado offline e os manifests instalados mantêm as mesmas qualificações. Limites do instalador/coletor: 64 bundles, 256 textos por bundle, 8 MiB por texto e 32 MiB por bundle.

O inventário resultante tem **1.010 componentes, 470 textos únicos e um arquivo de fonte WiX**. Mantém 40 fontes de catálogo, 41 builds PECL, dois pré-requisitos Microsoft e os sete achados históricos com suas disposições atuais. Os 169 novos registros não representam 169 licenças independentes nem todos os componentes ligados ao executável.

## Evidência e verificações

As evidências locais ficam em `build/verification/redis-imagick-licenses-20261021/`, ignorado pelo Git. Os dados de auditorias anteriores continuam preservados. A restrição de rename/junction do sandbox Windows afetou fixtures temporárias; as execuções com a permissão necessária passaram, sem alterar o comportamento de produção.

| Verificação | Resultado |
| --- | --- |
| RED antes da implementação de retenção/coletor | Falhas esperadas: helper ausente, catálogo sem suporte e limite de 64 incompatível com os 100 textos reais. |
| `go test ./internal/installer -count=1`, toolchain 1.26.10, diretório local isolado | Passou com o conjunto final 100/69. Inclui hash/kind/versão, bytes exatos, manifests anteriores, dados existentes, repetição e falha antes de escrever payload. |
| `go vet ./internal/installer` | Passou. |
| Ferramentas de licença e fixtures de assets legais | **51/51 passaram**, incluindo rejeição de alteração de hash/URL/escopo, arquivos ausentes/extras, aliases e links. |
| Unidade do leitor legal | **8/8 passaram**, incluindo leitura local, erro/retry, foco, fechamento e preservação integral de texto longo com CRLF/Unicode. |
| Gerador + `--check`, Go fixo offline | **1.010 componentes / 470 textos / uma fonte**, saídas determinísticas e atuais. |
| `npm run build` | Passou; permanece o aviso conhecido do Vite sobre chunk grande. |
| Wails 2.12.0, produção Windows amd64, `-webview2 browser -nosyncgomod` | Compilou sem executar o desktop; quatro arquivos legais completos e o frontend corrigido encontrados byte a byte no EXE e **36 módulos** conferidos contra o inventário. |
| Leitor legal no navegador, nuvem e tráfego externo bloqueados | Passou mantendo o limite original de 30 segundos; compara os dois textos completos por hash e verifica ausência de tráfego externo/erros e restauração do foco. |
| Conteúdo no índice Git | **690 hashes de entradas/saídas conferidos**, sem divergência. Backend e pastas locais preservados. |

O executável final de revisão é `build/bin/LaraEnv-redis-imagick-final-20261021.exe`; o primeiro candidato permanece preservado separadamente. Não substitui o pacote operacional nem foi iniciado. Seu hash e metadados ficam em `binary-validation.json` e `desktop-metadata.txt`. Não houve build de MSI, instalação/upgrade real em máquina limpa, build Redis/SDK ou teste de ABI Imagick nesta etapa.

O crescimento do agregado para 2,66 MB revelou um problema real de layout no leitor: o `<pre>` monolítico bloqueava a primeira pintura por cerca de 16 segundos e a leitura do teste levava mais 16 segundos. A rede entregava os arquivos em aproximadamente 11 ms; mudar o seletor ou retirar a quebra automática de linha não resolveu. A correção mantém os dois documentos completos no DOM, em blocos pequenos separados somente em quebras de linha, com `content-visibility:auto` para adiar o layout fora da tela. Não houve corte de texto ou aumento do timeout. O teste da versão compilada passou em **1,28 segundo**, preservando os hashes integrais e a navegação por teclado. O diagnóstico está em `browser-diagnosis/`; o teste de unidade também cobre texto longo com CRLF, Unicode e conteúdo HTML tratado como texto.

A conferência independente da versão real mediu **47 ms de layout** e validou o último trecho efetivamente pintado dentro da área de leitura, a busca no aviso final e a seleção atravessando blocos. A seleção de uma fixture CRLF/Unicode foi idêntica à de um `<pre>` monolítico; ambos omitem o mesmo terminador final pelo serializador do navegador, sem perda ou duplicação interna. Nenhum clipboard real foi acessado. O Chrome CDP expôs o agregado completo após seleção e o LICENSE completo após foco/paint; isso não substitui teste com NVDA/JAWS ou tecnologia assistiva nativa no WebView2. Provas em `browser-diagnosis/production-final-focus/results.json`, `production-crlf-fixture/results.json` e `diagnosis.md`.

Três revisões independentes conferiram os dados: Redis reconstruiu **194 intervalos**, os 100 textos e os cinco antigos contra o ZIP fonte; Imagick reconferiu os 69 textos, os oito anteriores e **166/166 DLLs idênticas ao SDK**; a revisão de código/coletor conferiu todos os 177 textos de ambos os registros e as qualificações geradas. Relatórios: `redis/independent-integration-validation.json`, `imagick-notices/integration-review.json` e `imagick-sources/independent-code-review.json`.

## Encerramento e limites concretos

A perda local dos avisos recuperados foi corrigida para os hashes examinados. A [revisão Redis](redis-license-closure.md) registra os termos compostos, dlfcn-win32 LGPL-2.1 ou posterior, SipHash CC0 e uma rota de fonte da seção 6(a), com projetos e dependências identificados. A política e o texto offline dão aviso da biblioteca e preservam os direitos de modificação/depuração. Um futuro distribuidor do binário precisa entregar a fonte/receitas efetivamente; um ZIP numa pasta ignorada não cumpre essa entrega.

A [revisão dos termos Imagick](imagick-license-closure.md) e a [rota de fonte/religação](imagick-source-route.md) recuperam a receita Windows, os patches e fontes históricas candidatas para 32 famílias, incluindo oito estáticas. FLIF inclui encoder e mantém LGPL-3.0 ou posterior. Permanecem lacunas concretas para fontes modificadas/religação, os pré-compilados X11/Xext e o texto Adobe citado por LibRaw. Portanto, não se declara liberado um mirror próprio desse ZIP nem os outros builds PHP não examinados.

Essas obrigações pertencem aos runtimes opcionais. A licença do código original do desktop continua Apache-2.0; o backend privado não foi vinculado nem incluído. As etapas anteriores de [publicação pgvector](runtime-publication-verification.md), segurança de dependências, revisão do snapshot e teste numa máquina Windows limpa mantêm seus próprios resultados e pendências.
