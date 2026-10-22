# Fechamento do catálogo de runtimes opcionais

Revisão de 21 de outubro de 2026, complementar à [matriz anterior](runtime-licenses.md). Abrange MySQL, Cmder, PHP, Apache Lounge, PostgreSQL EDB, Caddy, Mailpit, Node.js, Python, Nginx e pré-requisitos Microsoft. pgvector, PECL e Redis pertencem a frentes separadas. O backend privado ficou fora do escopo; nenhum runtime, executável baixado ou PHAR foi executado.

**Conclusão para o modelo atual:** os runtimes aqui examinados são obtidos pelo computador do usuário diretamente do fornecedor, ou fornecidos manualmente pelo próprio usuário. O código de `Manager.Install`/`download` em `internal/installer/installer.go` grava o download no cache local e extrai seus membros; não apresenta um servidor do projeto que forneça esses binários. As condições de fonte correspondente para um futuro mirror, MSI com runtimes ou release com binários MySQL/Cmder devem permanecer explícitas, sem serem transformadas em bloqueio automático à distribuição do código desktop. Isso é uma conclusão limitada a esse fluxo, não uma certificação jurídica de qualquer combinação futura de produtos.

As evidências novas estão em `build/verification/runtime-publication-20261021/catalog/`: respostas HTTP, bytes originais, fontes, textos legais, SBOMs, manifests e hashes. O diretório é evidência local ignorada, não material autorizado para publicação. SHA256 identifica bytes; igualdade com um checksum publicado não equivale a verificar assinatura ou correspondência fonte/binário.

## PHP: dois arquivos exatos recuperados

Os dois URLs antigos continuam retornando HTTP 404. Os mesmos patches, toolchains, arquitetura e variante NTS estão disponíveis nos arquivos oficiais. Não houve substituição por versão diferente.

| Build | URL oficial recuperado | SHA256 do ZIP | Conteúdo observado |
| --- | --- | --- | --- |
| 8.4.25 NTS vs17 x64 | [releases/archives/php-8.4.25-nts-Win32-vs17-x64.zip](https://windows.php.net/downloads/releases/archives/php-8.4.25-nts-Win32-vs17-x64.zip) | `43a8f67ed2e5223fafb21293c85976361808855405278cef2cf3037c3ae2529c` | 35.081.597 bytes, 85 membros; PHP-3.01 em `license.txt`; `readme-redist-bins.txt` de 747.271 bytes |
| 8.6.0beta3 NTS vs18 x64 | [qa/archives/php-8.6.0beta3-nts-Win32-vs18-x64.zip](https://windows.php.net/downloads/qa/archives/php-8.6.0beta3-nts-Win32-vs18-x64.zip) | `1a27dfa37a2155334858a1e4bf0e29b5b1c1209af0708d96f994177b5ba64775` | 40.194.808 bytes, 84 membros; BSD-3-Clause em `license.txt` e no README; `readme-redist-bins.txt` de 822.655 bytes |

Os redirects terminam em `downloads.php.net/~windows/`, mantendo os mesmos nomes. Os manifests `php-8.4.25-archive-inspection.json` e `php-8.6-beta3-archive-inspection.json` registram todos os membros e os textos originais. O SHA256 de `license.txt` é, respectivamente, `098bdc5b9808fc16bef4b7154e876ac08ca249549727cde3b3f3f3b431e27cca` e `15c880f6d18c2d6f3650c3e61a18939a00e4641d5360da5507c4afe96064c628`. A licença principal não substitui os termos das bibliotecas em cada `readme-redist-bins.txt`.

Ambos fornecem `extras/sbom/php.cdx.json`, `php.spdx.json` e `php.openvex.json`. O inventário declarado do fornecedor foi recuperado: 100 componentes CycloneDX/101 pacotes SPDX para 8.4.25; 107/109 para o beta. Cada SPDX tem 34 `licenseDeclared: NOASSERTION`, mas nenhum `licenseConcluded: NOASSERTION`; os campos de conclusão contêm expressões de licença. Esses campos são a avaliação do fornecedor, não uma conclusão independente desta revisão. Os documentos SPDX não têm inventário de arquivos (`files` vazio), portanto não demonstram correspondência de cada DLL byte a byte.

O relatório `php-sbom-inspection.json` contém hashes dos seis JSONs, contagens e expressões. Entre os termos declarados há LGPL para libbcmath, libmbfl, glib, libenchant2, libiconv, libqdbm e mpir; o beta também lista libheif sob LGPL-3.0-only. Freetype e zstd oferecem alternativas de licença na própria expressão. O OpenVEX trata de vulnerabilidades, não concede direitos autorais. Para o download atual, conservar os arquivos fornecidos e os SBOMs. Uma eventual redistribuição própria deve selecionar as alternativas aplicáveis e cumprir obrigações de fonte/relinking pertinentes; não se declara o ZIP inteiro permissivo por causa da licença principal do PHP.

**Integração concreta:** corrigir apenas esses dois URLs para os caminhos arquivados, atualizar as duas coberturas e os hashes de entrada da policy, acrescentar os manifests exatos e manter os 404 anteriores como histórico. Esta frente não altera catálogo ou policy.

## MySQL e Cmder: fonte identificada, obrigação delimitada

O texto da [GPLv2](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html) distingue executar o programa de distribuir cópias e exige uma rota válida de fonte correspondente na distribuição de binários. A [FAQ da FSF](https://www.gnu.org/licenses/gpl-faq.en.html#UnchangedJustBinary) esclarece que repassar binários recebidos também exige cumprir essas condições. Não é necessário forçar o usuário que apenas baixa do fornecedor a baixar também o fonte. Tampouco a mera referência a esses downloads demonstra que o backend privado deva ser publicado.

**MySQL Community 9.7.0/8.4.9/8.0.46.** Os manuais LICENSE das tags exatas já estavam preservados; foram confirmadas as refs oficiais de [mysql/mysql-server](https://github.com/mysql/mysql-server/tags), respectivamente nos commits `845d525d49c8027a4d0cdcc43372c96ba295c857`, `f9c88132a87a7d9e740e50bce2621999695bd3fe` e `0a7df2e4693d8f10901a26034ae6257699356e30`. As respostas da API estão em `downloads/mysql-*-tag.json`. As tentativas HEAD dos tarballs em `dev.mysql.com/get/Downloads/` retornaram 403 neste ambiente; isso não demonstra ausência dos fontes. Nenhum ZIP Windows MySQL foi inventariado nesta etapa.

Para o fluxo atual, manter a origem Oracle e seus avisos fornecidos. Para futuro mirror/bundle, selecionar e preservar o fonte correspondente completo da versão efetivamente distribuída, scripts/configurações de build e bibliotecas abrangidas, e implementar uma rota válida da GPLv2 seção 3. As tags e um link genérico não certificam sozinhos a correspondência dos ZIPs Windows nem a satisfação das condições do redistribuidor. As permissões adicionais de ligação e exceções do cliente continuam tendo seus escopos próprios.

**Cmder 1.3.25.** O [ZIP full exato](https://github.com/cmderdev/cmder/releases/download/v1.3.25/cmder.zip) foi inspecionado como dados: 165.667.993 bytes, 7.448 membros, SHA256 `9fa50466e9ba270b91ca915c4865cdc55ffd431b44507eef999506117b0a4ec6`. Ele e o mini anteriormente preservado (`c0611777a5bee9952cfb0439bd903efe49248de99823a3127563c55e7fa1a5d8`) conferem com o [hashes.txt oficial da release](https://github.com/cmderdev/cmder/releases/download/v1.3.25/hashes.txt). Os manifests novos conservam textos legais e metadados de versão: 125 entradas selecionadas no full e 17 no mini; as três entradas adicionais do mini são `.cmderver`, não três novas licenças.

Ambos identificam Clink 1.6.14, com GPLv3 em `vendor/clink/LICENSE`, ConEmu e clink-completions. O full também contém `vendor/git-for-windows/etc/package-versions.txt`, com Git `2.45.1.1.965b16798d-1` e versões dos componentes GNU/MSYS2. Não classificar o conjunto apenas como MIT do wrapper; a [FAQ oficial Git for Windows](https://gitforwindows.org/faq#licenses) descreve a composição sob várias licenças.

Há uma rota primária concreta para começar eventual preparação de fontes:

| Material preservado como dados | SHA256 |
| --- | --- |
| [Clink v1.6.14, fonte da tag](https://codeload.github.com/chrisant996/clink/zip/refs/tags/v1.6.14), 5.310.130 bytes, 741 membros incluindo premake/Readline/Lua | `3f3634ace040183a40fc20b98187ede7c4481809c935638fb4f4c587558d8f5f` |
| [Git Windows source package exato](https://github.com/git-for-windows/git/releases/download/v2.45.1.windows.1/mingw-w64-git-2.45.1.1.965b16798d-1.src.tar.gz), 11.425.525 bytes, inclui tarball fonte Git, PKGBUILD e arquivos do port | `2b814f915f04c2e1d31ec6981a5b951f98970efb82141b2574a2445b010af9c1` |
| [PKGBUILD da release Git](https://github.com/git-for-windows/git/releases/download/v2.45.1.windows.1/PKGBUILD.v2.45.1.windows.1), 10.027 bytes | `19d4fdc4547f2c7216d398d6314e961796d505d04cf16fb689451d3ff1ae948b` |

Essa evidência não é o fonte correspondente completo de **todos** os componentes do full, nem comprovação de correspondência independente dos executáveis Clink. Não foram executados build scripts. Uma futura redistribuição própria precisa cobrir também os demais pacotes do `package-versions.txt`, seus patches e licenças. Para continuar o download direto atual, essa preparação não é um pré-requisito de publicação do código desktop.

## Apache Lounge e EDB: arquivos manuais continuam individuais

A [página Apache Lounge](https://www.apachelounge.com/download/) obtida anuncia 2.4.69 VS18; o catálogo examinado identificava 2.4.62 VS17. A tentativa do URL exato antigo respondeu HTTP 200 com **2.451 bytes de HTML**, não um ZIP (`c7b7d9361862e57c1919ecd07e26e3491c667c16fe53a29d775ebec5a30b0d16`). Não foi contornado o mecanismo do fornecedor. LICENSE e NOTICE do httpd 2.4.62 continuam sendo evidência do projeto fonte, sem certificar o pacote Apache Lounge ou seus acompanhantes.

A [página EDB de binários PostgreSQL](https://www.enterprisedb.com/download-postgresql-binaries) continua móvel, com 18.6/17.11/16.15/15.19; isso não autentica os rótulos 18.3/17.9/16.13/15.17 do catálogo examinado. A identidade, hash e conteúdo legal do ZIP manual efetivamente escolhido continuam necessários para classificar aquele artefato. Não foi aplicado um EULA comercial EDB genérico a todos os arquivos PostgreSQL community. A divergência é uma limitação concreta dos metadados/download manual, não prova de que a licença do desktop esteja inválida.

Plano: manter seleção manual explícita e avisos originais; reconciliar rótulo e arquivo exato antes de afirmar que um arquivo manual corresponde ao patch escolhido. Só exigir inventário de dependências e condições de redistribuição próprias se houver proposta de hospedar ou empacotar esses ZIPs.

## Avisos aninhados e pré-requisitos

| Componente | Evidência aplicável agora | Limite para distribuição própria futura |
| --- | --- | --- |
| Caddy 2.11.4 | ZIP com LICENSE/README, checksum SHA512 oficial conferido. O [SBOM Windows oficial](https://github.com/caddyserver/caddy/releases/download/v2.11.4/caddy_2.11.4_windows_amd64.sbom) identifica exatamente `caddy.exe` SHA256 `5cb9ab71e5756ce72840b8234177a2f40c8b4ab47a806b8e841e2b784e9df62b`; contém 149 componentes. | Só um componente tem campo de licença nesse SBOM. Ele melhora a evidência de composição, mas não é um pacote completo de notices/licenças dos módulos. |
| Mailpit 1.29.7/1.29.6 | Ambos os ZIPs exatos fornecem LICENSE/README; metadados de release e go.mod das tags foram preservados. | Esses documentos não inventariam de forma independente todos os módulos compilados/recursos web embutidos; não declarar cobertura integral por MIT do projeto. |
| Node 24.15.0/22.22.2/20.20.2 | Os três ZIPs anteriores conferem com seus `SHASUMS256.txt` oficiais. Os manifests já registravam 175/228/231 arquivos legais/README selecionados. Preservar a árvore completa, incluindo npm/corepack quando fornecidos. | Não substituir LICENSE composto e avisos aninhados por um MIT curto; futuras alterações de payload exigem inventário próprio. Assinaturas dos checksums não foram verificadas. |
| Python embed 3.13.1/3.12.7 | LICENSE.txt composto de 33.861/36.874 bytes, incluindo termos de Microsoft Distributable Code e libffi; todos os arquivos fornecidos continuam necessários. | Os ZIPs também contêm `libcrypto-3.dll`/`libssl-3.dll`, mas os LICENSE.txt examinados não contêm texto OpenSSL. A licença composta não certifica por si só todas as DLLs. Conservar o original e revisar notices/fontes de acompanhantes antes de um mirror próprio; isso não foi expandido para auditoria de todos os binários. |
| Nginx 1.30.0/1.29.8 | ZIPs anteriores incluem `docs/LICENSE`; tarballs fonte exatos foram obtidos e seus textos legais preservados. | O BSD do Nginx e o fonte principal não comprovam automaticamente a versão/licenças de cada biblioteca vinculada no Windows. |
| Visual C++ Redistributable | Código examinado aponta o usuário para URLs Microsoft separados e não incorpora instaladores/DLLs. | A [Microsoft limita redistribuição aos usuários Visual Studio licenciados, nos termos aplicáveis](https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files). Verificar grant, REDIST, arquitetura e versão antes de incluir arquivos; não atribuir Apache-2.0 ao runtime. |
| WebView2 | Descoberta/recuperação usa instalação Microsoft; estratégia browser/bootstrap mantém o runtime separado. | [Evergreen e Fixed Version têm fluxos distintos](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution). Uma futura distribuição offline/fixa exige termos próprios. O nome da estratégia de bootstrap não autoriza substituir o WebView2 Runtime por Microsoft Edge Stable em produção. |

`checksum-validation.json` registra as comparações Node/Cmder/Caddy e a identidade do executável Caddy perante o SBOM. Os manifests anteriores continuam sendo as provas dos ZIPs preservados. Nenhum claim de completude de licenças foi inferido de um SBOM parcial ou da simples presença de LICENSE.

## Estado para integração

1. **Fechado com evidência nova:** existência e conteúdo legal dos dois ZIPs PHP exatos nos arquivos oficiais; inventário declarado e SBOMs desses builds; inventário do Cmder full e identificação versionada dos pontos de partida Clink/Git; igualdade dos checksums oficiais Node/Cmder/Caddy; vínculo do SBOM Caddy ao executável observado.
2. **Manter no modelo atual:** downloads do fornecedor, seleção manual quando necessária e conservação integral dos avisos originais. Não foi identificado nesta frente um binário desses produtos que precise ser adicionado ao release desktop para completar a tarefa.
3. **Condições de eventual redistribuição futura:** fonte correspondente e rota GPL de MySQL/Cmder, notices/obrigações de acompanhantes, grant Microsoft e ZIPs manuais exatos. Não apresentar esses itens como liberação geral nem como bloqueio artificial do código desktop atual.
4. **Não certificado:** autenticidade criptográfica dos publishers, correspondência fonte/binário, completude independente de dependências estáticas/acompanhantes, arquivos manuais não obtidos ou patches PHP não examinados. pgvector, PECL e Redis não recebem conclusão por esta revisão.
