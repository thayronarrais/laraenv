# Redis Windows 5.0.14.1 — avisos e rota de fonte, 2026-10-21

O build examinado contém **dlfcn-win32 sob LGPL 2.1 ou posterior**, além dos componentes BSD/MIT já identificados. Também contém SipHash com dedicação CC0 e avisos próprios de módulos Lua, Win32_Interop e outras contribuições. Portanto, a descrição do conjunto precisa ser composta; os cinco textos Redis existentes continuam corretos, mas não representam todos os termos do executável.

Esta etapa recuperou os avisos exatos e preparou uma **rota de fonte da seção 6(a) da LGPL 2.1 para revisão local**. O ZIP fonte da tag contém o código da biblioteca e do programa que a usa, os oito projetos Windows, dependências e recursos necessários identificados estaticamente. Não foi encontrada uma lacuna concreta de fonte de programa/biblioteca que obrigue obter os `.obj` originais. A entrega efetiva desse material e dos avisos aos destinatários ainda precisa ser integrada ao fluxo de distribuição.

O escopo é o desktop e o Redis específico abaixo. Produção foi somente lida. Nenhum EXE, DLL ou runtime foi executado; não houve build do Redis, instalação operacional, conexão a serviço, acesso ao backend ou publicação. Os arquivos novos desta frente são este relatório e `build/verification/redis-imagick-licenses-20261021/redis/`. As conclusões ampliam a investigação Redis do [seguimento anterior](runtime-companions-followup.md), sem apagar suas limitações históricas.

## Identidade e correlação do fonte

| Material | Bytes | SHA256 |
| --- | ---: | --- |
| [ZIP Redis Windows 5.0.14.1](https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.zip) | 12.617.669 | `018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf` |
| [ZIP fonte da tag v5.0.14.1](https://codeload.github.com/tporadowski/redis/zip/refs/tags/v5.0.14.1) | 2.742.099 | `c99c9182d8c0bd7a1bca0de92d05b7a4e0d4beef7461f6f1c07485e05649d95a` |
| `redis-server.exe`, também idêntico a `redis-check-aof.exe` e `redis-check-rdb.exe` | 1.856.000 cada | `79cd623a9999d75e944e8d2509547ddeee67a2c6c7b88d11d1137eb57fec10a0` |
| Pacote local de revisão `redis-5.0.14.1-source-route-review.zip` | 2.803.261 | `ea1eb105129a38e596c20787fd0543d8f601bfa5509396719ef359c66044d3ee` |

A [referência Git oficial](https://api.github.com/repos/tporadowski/redis/git/ref/tags/v5.0.14.1) aponta para **`ec77f72dce220609b67adfed67f7a58946747b56`**, também presente no comentário do ZIP fonte. O executável contém a versão 5.0.14.1, o prefixo Git `ec77f72d` e `jemalloc-5.2.1-redis`. O PDB fornecido contém `dlfcn.c`, `dlopen`, `dlclose`, `dlsym` e o mesmo prefixo Git. Os offsets, hashes e método de busca de bytes estão em `source-route-manifest.json`; caminhos privados de máquinas de build não foram reproduzidos no relatório.

Essas provas vinculam a investigação ao fonte oficial específico e permitem avaliar a rota de fonte. Não equivalem a compilação reproduzível nem a uma afirmação sobre toda configuração privada usada pelo produtor. Nove arquivos centrais recuperados pelas URLs raw da tag, incluindo os quatro módulos Lua, `dlfcn.c/.h` e os projetos Redis/Lua, são idênticos byte a byte aos do ZIP.

## Projetos e arquivos realmente selecionados

Foi lida a solução `msvs/RedisServer.sln`, configuração **Release|x64**. Ela seleciona RedisServer, RedisCli, RedisBenchmark, hiredis, Lua, geohash, Win32_Interop e jemalloc; este último mapeia para **Release-static|x64**. São **171 caminhos C/C++ distintos** listados para compilação, incluindo as unidades dos projetos de biblioteca. A expansão conservadora de includes locais cobre 390 arquivos. A análise não simula todo o pré-processador nem transforma headers condicionais POSIX/teste em componentes comprovadamente presentes no binário.

O [projeto RedisServer da tag](https://github.com/tporadowski/redis/blob/v5.0.14.1/msvs/RedisServer.vcxproj) compila expressamente `src/Win32_Interop/dlfcn.c`. `src/module.c` usa suas funções de abertura, consulta e fechamento de módulos, e os símbolos aparecem no PDB fornecido. O projeto Lua compila `lua_bit.c`, `lua_cjson.c`, `lua_struct.c`, `lua_cmsgpack.c`, `fpconv.c` e `strbuf.c`. As atribuições desses arquivos não são substituídas pelo `deps/lua/COPYRIGHT` geral.

O [README da própria tag](https://github.com/tporadowski/redis/blob/v5.0.14.1/README.md) documenta o jemalloc 5.2.1 modificado: as chamadas de alocação Windows são redirecionadas para os hooks do QFork. O código modificado e o projeto estão no ZIP; seu `COPYING` BSD de duas condições continua preservado exatamente. A modificação não introduz por si uma licença copyleft.

## Textos preservados para integração

`exportredis-notices.json` contém **100 arquivos exatos / 204.767 bytes**: 98 blocos distintos completos de copyright ou domínio público da união conservadora de projetos/includes e dois textos canônicos completos, LGPL 2.1 e CC0 1.0. Blocos idênticos são deduplicados por bytes, preservando todas as origens. Há **194 intervalos de fonte** reconstruídos e conferidos. Alguns avisos se sobrepõem aos cinco textos já existentes, e alguns headers só podem atuar em outros ramos condicionais; a retenção conservadora não afirma que cada um contribui código ao executável Windows.

Cada registro fornece `path`, `bytes`, `noticeSHA256`, `licenseDescription`, `evidencePath`, `sourceUrl`, `sourceSHA256`, `byteRanges` e `origins`. As URLs de código usam o commit imutável acima. Os comentários foram copiados sem normalizar LF/CRLF, sem reescrever atribuições e sem criar uma licença genérica substituta. A descrição é textual, pois um conjunto composto ou um aviso de domínio público não deve ser forçado a uma única expressão SPDX.

| Aviso prioritário / fonte na tag | Intervalo de bytes, início incluso/fim exclusivo | Bytes | SHA256 do aviso |
| --- | --- | ---: | --- |
| [Lua CJSON, Mark Pulford](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/lua_cjson.c), MIT | 0–1180 | 1.180 | `3828fc71155c3305c32f754690aab1b44b48bcdaef12de2b6967ff508ee1af1c` |
| [Lua BitOp, Mike Pall](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/lua_bit.c), MIT | 0–1305 | 1.305 | `d31d22f5b2410f75a618788e2c9c1cf1ac3d07cb58714c855d515f513b164adf` |
| [Lua struct, Lua.org/PUC-Rio](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/lua_struct.c), MIT completo no fim | 10222–11506 | 1.284 | `372ab0b3ad5f263b328fda3c639731ed55ed1b03cbb213e317b6d18cbb641a0e` |
| [Lua cmsgpack, Salvatore Sanfilippo](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/lua_cmsgpack.c), MIT completo no fim | 29790–31073 | 1.283 | `f54c52252c92713cf37ec0a671967e849bacd1ebc4bb843af58d60b3a0a602ae` |
| [fpconv, Mark Pulford](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/fpconv.c), MIT | 0–1191 | 1.191 | `3c2eb46b494f685584c350acd18b74c0a7e0331822510d37ed93cf5ef581005f` |
| [strbuf, Mark Pulford](https://github.com/tporadowski/redis/blob/v5.0.14.1/deps/lua/src/strbuf.c), MIT | 0–1179 | 1.179 | `d90df656f8ded2db5f3e3dda873349fe61d4df90f706f32767b2fa460807b498` |
| [dlfcn.c, Ramiro Polla e Timothy Gu](https://github.com/tporadowski/redis/blob/v5.0.14.1/src/Win32_Interop/dlfcn.c), LGPL 2.1 ou posterior | 0–840 | 840 | `f51db63b5bd9251d16964c5ffcb2cfa395d1ac3d469acf87ec5d7810fa5626c1` |
| [dlfcn.h, Ramiro Polla](https://github.com/tporadowski/redis/blob/v5.0.14.1/src/Win32_Interop/dlfcn.h), LGPL 2.1 ou posterior | 0–797 | 797 | `ca5a0042f8109d16db84aaebac01e7a648c87fe4eefa48e6eddace6a51ee52e3` |
| [LGPL 2.1 completa, GNU](https://www.gnu.org/licenses/old-licenses/lgpl-2.1.txt) | arquivo completo | 26.419 | `20e50fe7aae3e56378ebf0417d9de904f55a0e61e4df315333e632a4d3555d95` |
| [CC0 1.0 completa, Creative Commons](https://creativecommons.org/publicdomain/zero/1.0/legalcode.txt) | arquivo completo | 7.048 | `a2010f343487d3f7618affe54f789f5487602331c0a8d03f49e9a7c547cf0499` |

O inventário integral também conserva as autorias BSD de Microsoft Open Technologies/Win32_Interop, geohash, hiredis/SDS, linenoise, CRC e ordenação parcial; o aviso original de Berkeley e a atribuição da adaptação em `pqsort.c`; as declarações originais de domínio público em SHA-1; e o cabeçalho SipHash com Jean-Philippe Aumasson, Daniel J. Bernstein e Salvatore Sanfilippo. Dois comentários Microsoft Corporation de headers de compatibilidade ficam preservados junto ao cabeçalho BSD de duas condições do MS Open Tech do arquivo, sem inferir uma autorização geral para distribuir o SDK.

O texto liblzf oferece **BSD de duas condições OU GPL 2 ou posterior**, e o original mantém a alternativa BSD. Esta revisão usa essa alternativa permissiva e conserva o aviso dual completo. Não foi identificada exigência de selecionar GPL ou fornecer seu texto por essa escolha; isso não elimina a obrigação LGPL independente de dlfcn-win32.

## LGPL 2.1: rota de fonte e religação

A [seção 6(a) da LGPL 2.1](https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html#SEC6) permite fornecer a biblioteca, incluindo as modificações usadas, e o programa que a utiliza em **objeto e/ou fonte**, permitindo ao destinatário modificar a biblioteca e religar o executável. Portanto, fonte utilizável completo pode cumprir essa parte sem os objetos originais. A seção 6 também exige aviso destacado da biblioteca/licença, cópia da LGPL e termos que permitam modificação para uso próprio e engenharia reversa para depurar essas modificações. Avisos isolados não cumprem a rota de fonte; acesso equivalente em download deve ser implementado conforme a seção 6(d), quando escolhida.

`source-route-manifest.json` lista e confere os **909 arquivos** do ZIP original, os projetos, referências, recursos e comandos originais. O pacote local de revisão inclui o ZIP fonte **inalterado**, os 100 textos, esse manifest, a exportação e cópias exatas das instruções upstream. A verificação encontrou:

- Todos os oito `.vcxproj` e suas referências locais de código/dependências estão presentes, incluindo `dlfcn.c/.h`, Lua, hiredis, geohash, jemalloc modificado e Win32_Interop. Não existe import local obrigatório de `.props/.targets` ausente; os imports `Microsoft.Cpp.*` pertencem ao toolchain, e o arquivo de preferências do usuário é opcional.
- `src/release.h` é o único item listado ausente. O `src/mkreleasehdr.sh` incluído e o README explicam como produzi-lo ou criá-lo manualmente. Ele define o prefixo Git, o estado de alterações e o identificador de build. O ZIP não contém `.git`: para gerar metadados coerentes, usa-se checkout do commit exato ou criação manual documentada. O host/horário original não precisa ser reproduzido para religar uma biblioteca modificada.
- `EventLog.mc`, `EventLog.h`, `resources/EventLog.rc`, `EventLog.res` e `EventLog_MSG00409.bin` estão presentes. O projeto inclui o comando original com `mc.exe`, `rc.exe` e `link.exe`. O recurso de versão `RedisForWindows.rc` e `resource.h` também estão presentes.
- As instruções upstream pedem Visual Studio 2019 16.2.5 ou posterior com C/C++, Windows SDK 10 e Git Bash/Cygwin para o script. Esses são pré-requisitos externos de compilação; não foram baixados nem executados. A parte MSI de `msvs/Build.bat` pertence ao empacotamento e não é necessária para religar RedisServer, RedisCli, RedisBenchmark ou gerar EventLog.

Todos os projetos selecionados usam toolset v142 e CRT estático (`MultiThreaded`, `/MT`). Isso é evidência da configuração, **não** prova de impossibilidade de religação ou exigência automática de fornecer fontes do compilador/CRT. A LGPL prevê exceção para componentes normalmente distribuídos com o compilador/sistema, nos limites do seu texto. Nenhuma compilação foi feita nesta etapa, de modo que não se afirma sucesso de build nem execução de um Redis com biblioteca substituída. O resultado verificável é a disponibilidade dos fontes, projetos, recursos e receitas identificados, com a ausência gerada explicada.

## Microsoft: o que os dados permitem concluir

O ZIP contém seis arquivos PE: três executáveis idênticos do servidor/checks, CLI, benchmark e `EventLog.dll`. Não contém um instalador VC Redistributable nem DLL CRT separada; a inspeção estática anterior lista importações de bibliotecas do Windows. Não foi encontrada uma autorização Microsoft adicional concreta que precise ser comprada ou obtida apenas por causa de `/MT` ou de dlfcn-win32.

Os [termos oficiais do Visual Studio Community 2019](https://visualstudio.microsoft.com/license-terms/mlt031819/) preveem distribuição de código redistribuível e permissão para distribuidores do aplicativo, com condições. A [lista REDIST de 2019](https://learn.microsoft.com/en-us/visualstudio/releases/2019/redistribution) vincula essa permissão à licença aplicável. Isso oferece uma rota de distribuição, sem demonstrar a licença privada usada pelo produtor ou tornar código Microsoft aberto. Não foi verificada documentação privada de aquisição nem acrescentada licença Microsoft genérica aos suplementos. Caso o projeto passe a fornecer arquivos do SDK/CRT separadamente, deve conferir os arquivos e termos concretos dessa nova entrega.

A cópia DOCX oficial foi lida como dados e preservada em `primary/VS-Community-2019.docx`: **43.685 bytes**, SHA256 `3dc17ec1490b436a97816aa4b06cc08eac9e37933a2813dbfa1886eaf23bde08`. As páginas e URLs resolvidas, tamanhos, hashes e datas estão em `primary-retrieval.json`. A análise de distribuição não inferiu incompatibilidade da simples presença de CRT estático, nem uma obrigação de disponibilizar seu código fonte sob LGPL.

## Encerramento por lacuna

| Lacuna | Resultado desta etapa | Ação concreta na integração |
| --- | --- | --- |
| Avisos Lua adicionais | Textos completos recuperados dos arquivos realmente listados pelo projeto, com intervalos e hashes exatos. | Reter o export junto aos cinco documentos atuais, somente para o hash Redis examinado; testar preservação e dados existentes. |
| Autoria Win32_Interop/jemalloc e demais fontes | Inventário Windows completo de projetos; cabeçalhos/autorias adicionais conservados; fonte modificado jemalloc presente. | Usar descrição composta e preservar os textos originais. A retenção conservadora não deve virar alegação de composição binária de headers condicionais. |
| dlfcn-win32 LGPL | Biblioteca identificada no projeto e no PDB; avisos e texto completo preservados; rota de fonte 6(a) preparada e sem lacuna de código fonte identificada estaticamente. | Dar aviso destacado, manter direitos de modificação/depuração e entregar o fonte/receitas de modo efetivo, pela rota escolhida na seção 6. |
| SipHash CC0 / liblzf | Cabeçalho com todas as autorias e CC0 completa; aviso dual liblzf preservado e alternativa BSD identificada. | Manter esses textos; não classificar todo Redis como GPL nem somente BSD/MIT. |
| MSVC e futura redistribuição | Nenhum VC Redist separado no ZIP; fonte/toolchain e termos oficiais distinguidos. Não surgiu impedimento concreto deduzível de `/MT`. | Para uma entrega que acrescente SDK/CRT ou altere os binários, avaliar seus termos e arquivos reais; a evidência atual não fornece um novo grant Microsoft. |

Para o **download atual direto upstream**, pode-se encerrar a busca dos avisos adicionais e a identificação da rota de fonte deste ZIP. A preservação local só estará encerrada depois de integrar os textos por hash e validar sua retenção. O fluxo não hospeda nem reempacota o binário no release do desktop; o suplemento não corrige silenciosamente o ZIP publicado pelo produtor.

Para **mirror/MSI/ZIP redistribuído pelo projeto**, o material local é uma base concreta de fonte/religação e avisos, mas a entrega aos destinatários ainda deve existir e respeitar a LGPL. Não se declara concluída uma publicação que esta frente não fez. Não há motivo, com os dados obtidos, para exigir os `.obj` originais ou build bit a bit; também não se pode marcar a obrigação da seção 6 como satisfeita apenas porque um arquivo permanece numa pasta ignorada de auditoria.

Validação final: **100/100 textos**, **194/194 intervalos**, **5/5 documentos existentes**, **909/909 arquivos de fonte**, **9/9 comparações raw oficiais** e o ZIP de revisão com o fonte original intacto passaram em `validation.json`. O script de validação opera apenas sobre bytes, XML e ZIP. Código de produção, política, gerados, backend e assets publicados não foram alterados por esta frente.
