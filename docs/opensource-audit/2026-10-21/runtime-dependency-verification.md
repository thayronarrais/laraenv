# Validação de runtimes e dependências — 2026-10-21

Registro da etapa anterior à publicação. Os quatro pgvector e seus sidecars foram corrigidos posteriormente no mesmo dia, e houve novo fechamento de evidência PHP/PECL; veja a [validação mais recente](runtime-publication-verification.md). As contagens e limites abaixo preservam a base daquela etapa.

Continuação sobre `00b2b71`, na branch `codex/workspace-source-control`. Escopo: desktop, ferramentas de build e licenças dos downloads opcionais. Backend privado, credenciais, runtimes operacionais e dados existentes ficaram fora das alterações. Evidências locais ignoradas: `build/verification/runtime-licenses-dependencies-20261021/`. O [registro inicial](verification.md) permanece histórico.

## Retenção e inventário

Os testes de instalação PECL preservam bytes CRLF exatos, licenças aninhadas, OFL, README e hashes. Verificam idempotência, documento diferente já existente, ausência de aviso upstream, entradas com traversal/backslash/ADS/aliases Windows, symlinks e limites de tamanho. Os casos inseguros falham antes de modificar a DLL existente. As fixtures pgvector/Redis conferem hashes oficiais, limites de versão e preservação de LICENSE já presente. O teste de namespace reservado demonstrou a sobrescrita antes da correção e passou depois dela, incluindo aliases de caixa, barra e ponto terminal.

`go test -p 2 ./... -count=1 -timeout=12m` passou nos **42 pacotes com testes**, com Go **1.26.10** e diretório LaraEnv temporário isolado. `go vet ./...` passou. Nenhum runtime baixado ou aplicativo desktop foi executado; fixtures SSH usam apenas recursos locais de teste.

As **41 fixtures Node** passaram: 37 de geração/licenças/PHAR/RTF e quatro de cópia para o frontend. O gerador e seu `--check` verificaram **833 componentes, 296 textos exatos e um arquivo de fontes WiX**. A cobertura inclui 180 módulos Go, 528 pacotes npm, 40 fontes do catálogo, 41 builds PECL, dois pré-requisitos e seis avisos embutidos. Estes números incluem escopos diferentes e não são uma contagem de componentes vinculados ao executável. Os sete avisos históricos da política permanecem visíveis; metadados descritivos dos downloads não são indevidamente tratados como SPDX ou liberação de redistribuição.

A checagem RTF e a checagem dos assets legais locais passaram. A revisão independente do coletor não encontrou defeitos concretos. A revisão do instalador encontrou a possibilidade de ZIP sobrescrever avisos suplementares, corrigida com preflight e teste de regressão.

## Dependências e interface

Audits finais contra o registro oficial npm retornaram **zero vulnerabilidades** no frontend completo, em suas dependências de produção e nas ferramentas de screenshots. Nenhuma versão de dependência npm de runtime mudou. A migração Tailwind **4.3.3** usa o adaptador PostCSS correspondente, conserva PostCSS **8.5.23** e remove as cadeias vulneráveis da versão 3 sem overrides forçados.

O grafo Go completo foi de **163 para 180 módulos** e de **27 para um ID**. As versões corrigidas e os scans foram conferidos contra o banco oficial, modificado em `2026-10-21T00:27:29Z`. Desktop de produção Windows, exports SSH completos e CLI Wails reconstruída tiveram **zero pacotes afetados importados e zero achados de símbolos**, conservando apenas o aviso de módulo [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), OpenPGP sem correção publicada. `go mod verify` e a compilação da CLI passaram. O build/release agora usa a CLI com o grafo do checkout e `-nosyncgomod`; executar `go mod tidy` exige preservar/revalidar os requisitos de segurança das ferramentas.

A comparação Chromium 155 cobriu **24 pares**, com dashboard, projetos, configurações e workspace, larguras 1366/900 e temas midnight/nebula/aurora. Geometria, fontes e altura de linha amostradas coincidiram em todos. Em 23 pares não houve pixels com diferença acima de dez níveis por canal. A variação do logo no primeiro dashboard reproduziu exatamente a máscara de um controle da mesma baseline contra si própria, com zero pixels exclusivos. Pequenas diferenças de arredondamento alfa e serialização CSS não alteraram a geometria. Uma regressão real de line-height dos indicadores foi corrigida antes do build final. Limites: fixtures sintéticas em inglês, três temas e Chromium headless; não cobre todos os temas e estados do produto. Tailwind 4 requer WebView2 baseado em Chromium 111+.

Frontend: **771 testes em 88 arquivos passaram**. A suíte de navegador sobre o frontend compilado também passou: **104 testes em 18 arquivos**, incluindo o leitor offline de licenças com conteúdo integral. TypeScript passou no build de produção; os quatro catálogos de idioma passaram. ESLint terminou com **zero erros e os mesmos 43 avisos existentes**. O build conserva o aviso de chunk acima de 500 kB.

## Artefatos locais

O frontend compilado final inclui os avisos novos. O executável de revisão foi reconstruído com a CLI Wails do grafo corrigido, Go 1.26.10 e tags `desktop,wv2runtime.browser,production`. A inspeção estática encontrou os bytes completos de LICENSE, NOTICE, THIRD_PARTY_NOTICES e inventário incorporados; seus **36 módulos** correspondem às versões inventariadas. O desktop não foi iniciado.

O empacotador real gerou MSI, portátil e executável, com três sidecars SHA256, em uma pasta nova. A extração para inspeção com WiX conferiu por hash os **seis payloads do MSI**; os **oito arquivos do ZIP portátil** e os três sidecars também corresponderam às entradas revisadas. Licenças/fontes WiX 5.0.2 estão presentes. Versão MSI 0.5.7 é somente fixture local; o executável mantém versão interna de desenvolvimento. Não houve instalação ou publicação. O NSIS não foi recompilado nesta continuação.

Quatro ZIPs pgvector corrigidos foram preparados separadamente como candidatos locais. Cada um conserva todos os 43 membros originais e acrescenta somente o LICENSE upstream fixado, com seu sidecar SHA256. Os ZIPs originais e os assets públicos continuam inalterados. O [relatório de runtimes](runtime-licenses.md) conserva a necessidade de atualização pública e de comprovar proveniência/obrigações dos acompanhantes.

A leitura dos arquivos preparados no índice Git conferiu **335 digests** de entradas, textos legais e saídas, sem divergências. Isso verifica que os atributos de fim de linha preservam os hashes gerados e os seis originais upstream no snapshot. Nenhum arquivo do backend ou evidência ignorada foi incluído no índice desta etapa; essa checagem não substitui o build numa máquina limpa.

## Limites e próximos passos

As suítes Windows precisaram de execução fora do sandbox para criar links/renomear arquivos temporários. As primeiras tentativas com relatório incompleto não foram usadas como evidência de aprovação; as contagens vêm dos relatórios JSON completos. As consultas npm inicialmente sem DNS foram repetidas com sucesso no registro oficial.

Ainda faltam o teste de instalação/upgrade em Windows limpo, a revisão do snapshot público exato/CI e a publicação autorizada dos assets corrigidos. Avisos de acompanhantes PECL/Redis, rotas de fontes MySQL/Cmder e downloads manuais/não examinados mantêm suas condições. Abrir o desktop não libera automaticamente esses binários, o runtime Microsoft ou o histórico privado. Não houve push, release, alteração de visibilidade ou reescrita de histórico.
