# Acompanhantes PECL e Redis — seguimento de 2026-10-21

Esta revisão aprofunda as lacunas da [matriz de runtimes](runtime-licenses.md), com prioridade para **MongoDB 1.20.1 / PHP 8.4 NTS VS17 x64** e **memcached 3.3.0 / PHP 8.3 NTS VS16 x64**. Os textos ausentes desses exemplos foram recuperados de fontes primárias versionadas e preservados sem alterações. Isso permite preparar suplementos vinculados ao **SHA256 exato do ZIP**, sem declarar conformidade de todo o pacote ou estender a conclusão aos demais builds.

A produção foi somente lida. Nenhuma DLL, EXE, PHAR ou runtime foi executado; nenhum serviço, credencial ou backend foi acessado. Esta etapa não alterou código, política, gerados ou assets públicos. As evidências novas ficam em `build/verification/runtime-publication-20261021/companions/`; os ZIPs originais anteriores continuam inalterados. A publicação pgvector pertence a outra frente e não é reavaliada aqui.

## Resultado por lacuna e forma de distribuição

| Lacuna | O que ficou estabelecido | Download direto upstream no computador do usuário | Antes de mirror, MSI ou ZIP redistribuído pelo projeto |
| --- | --- | --- | --- |
| MongoDB: notices libbson/libmongoc ausentes | Versões 1.28.1 e textos exatos recuperados; libmongocrypt 1.11.0 também identificado. Há sete textos candidatos, incluindo notices comuns/kms e utf8proc. | Pode fechar a perda local dos avisos examinados com suplemento condicionado ao hash do ZIP; conservar também os quatro documentos que já estão no ZIP. | Entregar o conjunto completo de avisos aplicáveis e examinar dependências externas reais, como OpenSSL/PHP/CRT. Não afirmar que o suplemento prova toda a composição do binário. |
| memcached: libmemcached/libhashkit sem licença dedicada | As quatro DLLs acompanhantes são idênticas às do SDK oficial libmemcached-awesome 1.1.4. O SDK contém a licença BSD-3-Clause exata. | Pode fechar a ausência local da licença desses quatro acompanhantes para o ZIP examinado, com suplemento condicionado ao hash. Conservar PHP LICENSE, FastLZ LICENSE e demais documentos originais. | Incluir essa licença e os demais avisos aplicáveis; a conclusão não cobre outras versões, séries PHP ou bibliotecas opcionais de outro build. |
| Imagick: numerosas DLLs e possível fonte correspondente | Todos os 166 acompanhantes são idênticos ao SDK oficial ImageMagick 7.1.0-18. Versões de várias DLLs foram lidas como dados. | A retenção dos quatro documentos do ZIP evita a perda já observada. A origem do conjunto de DLLs está rastreada; o pacote não passa a ter uma única licença permissiva. | Permanecem necessários inventário de termos dos acompanhantes e uma rota de fonte/build/relinking quando aplicável. Não espelhar este pacote com base apenas em LICENSE.ImageMagick ou no match do SDK. |
| Redis Windows: cinco textos omitidos pelo ZIP | Tag/fonte, fingerprint do binário e projetos Windows corroboram o fork 5.0.14.1 e jemalloc 5.2.1 modificado. Os cinco textos existentes continuam corretos para esse escopo. Há avisos adicionais em módulos Lua compilados. | Os suplementos existentes resolvem as omissões nomeadas, sem converter o fluxo de download upstream em aprovação de redistribuição. | Completar os avisos dos módulos Lua efetivamente compilados e a revisão do conjunto Windows; não chamar os cinco textos de inventário completo. |

O fluxo atual de download direto não hospeda nem reempacota esses ZIPs num release LaraEnv. Preservação local de avisos, licença principal do projeto e conformidade de uma redistribuição concreta são conclusões diferentes. O suplemento não modifica o arquivo publicado pelo upstream nem autentica seu publisher.

## Identidades dos arquivos examinados

| Arquivo | SHA256 |
| --- | --- |
| [MongoDB 1.20.1 / PHP 8.4](https://windows.php.net/downloads/pecl/releases/mongodb/1.20.1/php_mongodb-1.20.1-8.4-nts-vs17-x64.zip) | `763efd7317b1153729b39031433fab008ad1c42fd46890dfa0db9765494bc588` |
| `php_mongodb.dll` desse ZIP | `f073b983bb6da9d09f80f1b10dc5ee151c6f24e60f0dc17f094182dcabb97c48` |
| [memcached 3.3.0 / PHP 8.3](https://windows.php.net/downloads/pecl/releases/memcached/3.3.0/php_memcached-3.3.0-8.3-nts-vs16-x64.zip) | `787f5311a6855002945c7dacc6295fda0ca6f77182fe7f16b4ef95780d16764b` |
| [SDK libmemcached-awesome 1.1.4 VS16 x64](https://windows.php.net/downloads/pecl/deps/libmemcached-1.1.4-vs16-x64.zip) | `d64aff6162f874394b11b649a6d05ee53cf5454595adf23d1b9f7493e26dd692` |
| [Imagick 3.7.0 / PHP 8.3](https://windows.php.net/downloads/pecl/releases/imagick/3.7.0/php_imagick-3.7.0-8.3-nts-vs16-x64.zip) | `4204071decbb06dce9f212a63ace0aa0c1b48fd344176b8274cd7d32ab674c71` |
| [SDK ImageMagick 7.1.0-18 VC15 x64](https://windows.php.net/downloads/pecl/deps/ImageMagick-7.1.0-18-vc15-x64.zip) | `3684a58b0896e2a55995029fa92cc13bd1ac778e03cdf8682c4369bbef86be9e` |
| [Redis Windows 5.0.14.1](https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.zip) | `018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf` |
| [Fonte Redis v5.0.14.1](https://codeload.github.com/tporadowski/redis/zip/refs/tags/v5.0.14.1) | `c99c9182d8c0bd7a1bca0de92d05b7a4e0d4beef7461f6f1c07485e05649d95a` |

### MongoDB: recuperar os documentos correspondentes, não substituir por Apache genérica

O `THIRD_PARTY_NOTICES` de 3.644 bytes do ZIP remete a documentos libbson/libmongoc que não estão no próprio ZIP. O [TGZ PECL 1.20.1](https://pecl.php.net/get/mongodb-1.20.1.tgz), SHA256 `614e57594918feb621f525e6516d59ce09b78f5172355ba8afb6c2207c1ce900`, também omite esses documentos aninhados; baixar esse TGZ sozinho não resolve a lacuna.

O fonte PECL fixa libmongoc/libbson **1.28.1** e libmongocrypt **1.11.0**. O [release oficial](https://pecl.php.net/package-info.php?package=mongodb&version=1.20.1) registra o upgrade do C Driver para 1.28.1. A DLL contém as strings dessas versões e caminhos das respectivas bibliotecas. Foram comparados os arquivos comuns com os tags upstream: **431/431** arquivos do C Driver e **174/174** de libmongocrypt são idênticos, sem divergência. São provas de correlação da versão e do fonte distribuído, sem equivaler a rebuild reprodutível da DLL.

O recurso Windows `FileVersion` da DLL é **8.4.1**, associado ao build PHP; ele não deve ser usado como versão de libmongoc. As importações PE incluem `libcrypto-3-x64.dll` e `libssl-3-x64.dll`, ausentes desse ZIP PECL: os avisos abaixo não cobrem automaticamente o build OpenSSL fornecido pelo PHP ou por outro instalador.

| Texto upstream exato para suplemento | Bytes | SHA256 |
| --- | ---: | --- |
| [C Driver 1.28.1 COPYING](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/COPYING), Apache-2.0; também idêntico ao COPYING de kms-message | 10.174 | `0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594` |
| [C Driver THIRD_PARTY_NOTICES](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/THIRD_PARTY_NOTICES), coleção composta integral | 8.959 | `2f397b6b524dadf7385fc0fac003fd713781cf84adb146fd85eecd8351defbe5` |
| [libbson THIRD_PARTY_NOTICES](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/src/libbson/THIRD_PARTY_NOTICES), aviso jsonsl MIT | 1.275 | `2dd83c6556d2306093c2d4fcb5bce6fde23d26195a6e8f695f96dbc7d8a2efea` |
| [libmongoc THIRD_PARTY_NOTICES](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/src/libmongoc/THIRD_PARTY_NOTICES), aviso Curl do backend secure-channel | 1.450 | `17b2daac1485adea3a60d8a11ce780aa267dfc904d71e0fa5ab18e9165405f72` |
| [kms-message THIRD_PARTY_NOTICES](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/src/kms-message/THIRD_PARTY_NOTICES), ISC e IBM | 2.237 | `130021af7db9aaeda7cd68821f06ecd9cf1a4e89e9e1b6d77c0d0fbca1bd89a0` |
| [utf8proc 2.8.0 LICENSE.md](https://raw.githubusercontent.com/mongodb/mongo-c-driver/1.28.1/src/utf8proc-2.8.0/LICENSE.md), MIT e termos dos dados Unicode | 5.125 | `3b510150d34f248a221bb88e1d811238d6c6c18b51231822c42974c39bb07256` |
| [libmongocrypt 1.11.0 LICENSE](https://raw.githubusercontent.com/mongodb/libmongocrypt/1.11.0/LICENSE), Apache-2.0 | 11.356 | `43070e2d4e532684de521b885f385d0841030efa2b1a20bafb76133a5e1379c1` |

Todos os sete textos foram conferidos byte a byte entre o ZIP do fonte versionado e a URL raw oficial. A coleção comum inclui avisos uthash, common-b64/IBM, CSS, zlib, MD5 e utf8proc. Conservá-la inteira evita trocar esse conteúdo por uma licença MIT/Apache curta. Preservar o notice secure-channel fornecido pelo upstream não afirma que esse backend específico esteja compilado no DLL que importa OpenSSL.

Os fontes PECL 1.16.2 e 2.5.2 foram lidos para evitar extrapolação: eles declaram outras versões de bibliotecas. Não foram examinados todos os seus ZIPs Windows, e os sete textos acima **não** devem ser aplicados indiscriminadamente a esses builds.

### memcached: vínculo exato com o SDK e uma licença recuperada

O próprio `php_memcached.dll` e `libmemcached.dll` identificam **libmemcached-awesome 1.1.4**. A versão não foi inferida do nome genérico `libmemcached`. As quatro DLLs do ZIP são idênticas às do SDK oficial:

| DLL | SHA256 |
| --- | --- |
| libmemcached.dll | `17aced04821d83bfc836ef2096e87c4403a0f91e677ad9c4f7a65ac9145532fb` |
| libhashkit.dll | `968c46e271702e20f98278221182a409d8a7c2717ab46e19ccdb9f31716bde2b` |
| libmemcachedprotocol.dll | `2fc19ea97813ab16f66a994f344ef92688b9b9d78762f60be2020a5f7070a489` |
| libmemcachedutil.dll | `8d3c4052a51bf354725950c1e40b2bc57c3bbcc3f944a0bc8a7492a070a36c69` |

O arquivo **`share/doc/libmemcached-awesome/LICENSE`** do SDK tem **1.653 bytes**, SHA256 **`a2052b7c3f836f4c48fc2763e88ead4bc7ab62f8e23bf406fb0f3b22b982dd5b`**, e contém BSD-3-Clause com os copyrights Brian Aker/DataDifferential e Michael Wallner/Awesome. É o texto recomendado para o suplemento desse ZIP. O [LICENSE do tag 1.1.4](https://raw.githubusercontent.com/awesomized/libmemcached/1.1.4/LICENSE) tem 1.624 bytes, SHA256 `65013ec8b416f28ae838ed3f23ea3ae47e5dbc70fe8492f39338e389a7970fd9`: é o mesmo texto após normalizar CRLF/LF, mas esses bytes e hashes não devem ser intercambiados.

Isso fecha a identificação e a recuperação da licença dedicada dos quatro acompanhantes examinados. Não substitui a PHP LICENSE da extensão, a licença FastLZ já fornecida, nem prova a composição de memcached 3.4.0/3.2.0 ou de outro build PHP.

### Imagick: origem binária rastreada, termos e fonte ainda condicionais

O `config/configure.xml` fornecido informa **ImageMagick 7.1.0-18**, build de 2021-12-14 x64. Os **166 acompanhantes DLL**, incluindo módulos IM_MOD e CORE_RL, são idênticos aos do SDK oficial; só `php_imagick.dll` não pertence ao SDK. A comparação inclui cada hash em `imagick-sdk-byte-comparison.json`.

Recursos PE e strings corroboram, entre outros: GLib 2.64.3, FriBidi 1.0.10, cairo 1.16.0, FreeType 2.10.4, HarfBuzz 2.8.1, librsvg 2.40.20 e liblqr 0.4.2. O recurso de LibRaw informa 0.20.0.1, mas uma string interna informa `0.21.0-Snapshot202101`; Pango mostra recurso 1.45.3 e string 1.45.2. Essas diferenças impedem substituir a investigação por um conjunto de tags presumido.

Foram preservados como evidência os textos versionados [GLib 2.64.3 COPYING](https://raw.githubusercontent.com/GNOME/glib/2.64.3/COPYING), 26.530 bytes, SHA256 `dc626520dcd53a22f727af3ee42c770e56c97a64fe3adb063799d8ab032fe551`, e [FriBidi 1.0.10 COPYING](https://raw.githubusercontent.com/fribidi/fribidi/v1.0.10/COPYING), 26.440 bytes, SHA256 `32434afcc8666ba060e111d715bfdb6c2d5dd8a35fa4d3ab8ad67d8f850d2f2b`. Seus termos LGPL não desaparecem porque a extensão Imagick ou a biblioteca principal possui outra licença. Ainda é preciso obter a rota de fonte/build e as condições de relinking aplicáveis aos binários concretos antes de distribuí-los pelo projeto.

O SDK também fornece `bin/NOTICE`, **57.754 bytes**, SHA256 `20223c0bf2fa31beda1f640453360db73d6626c518a2a972053ff7356107983e`. Ele foi preservado, mas cita programas opcionais como ffmpeg/dcraw/hp2xx que não aparecem como EXEs nesse ZIP PECL. Seu texto não justifica classificar todas as DLLs do PECL como GPL nem serve de inventário preciso de cada acompanhante.

O `tests/OFL.txt` do ZIP tem 4.527 bytes e SHA256 `b8739da5d3c5ba6b109328268095bf06509348498f4779ef7c8e71878d7c6539`; identifica Anonymous Pro Minus e Mark Simonson sob OFL-1.1. Nenhuma entrada `.ttf`/`.otf` foi encontrada nesse ZIP. O aviso deve continuar preservado, sem inventar a presença de uma fonte binária não observada.

### Redis Windows: avisos conhecidos corretos, módulos Lua adicionais

O ZIP de fonte tem o comentário Git `ec77f72dce220609b67adfed67f7a58946747b56`. O binário `redis-server.exe` contém o prefixo `ec77f72d`, versão 5.0.14.1 e `jemalloc-5.2.1-redis`; os projetos Windows referenciam `deps/jemalloc-5.2.1`, hiredis, Lua e Win32_Interop. O projeto Win32_Interop explica a geração de `EventLog.dll` como recurso de mensagens. Esses dados corroboram o release, sem constituir rebuild ou inventário estático completo.

Os cinco textos já preservados continuam correspondendo ao tag: fork `license.txt` (BSD-3-Clause), Redis `COPYING` (BSD-3-Clause), jemalloc `COPYING` (BSD-2-Clause), Lua `COPYRIGHT` (MIT) e hiredis `COPYING` (BSD-3-Clause). Seus hashes permanecem registrados na política existente e foram reconferidos no ZIP do fonte.

Há uma limitação concreta além desses cinco arquivos: o [projeto Windows Lua](https://github.com/tporadowski/redis/blob/v5.0.14.1/msvs/lua/lua/lua.vcxproj) compila `lua_cjson.c`, `lua_struct.c`, `lua_cmsgpack.c` e `lua_bit.c`. Seus textos de copyright/permissão próprios não aparecem no `deps/lua/COPYRIGHT` principal. As cópias exatas dos quatro arquivos fonte, com os avisos in situ, foram preservadas em `redis-inline-vendored-notices.json`; por exemplo, Lua CJSON atribui Mark Pulford e Lua BitOp atribui Mike Pall. Isso fornece uma ação verificável para a próxima integração: conservar também os avisos originais desses módulos, conferindo seus limites de bytes, em vez de substituí-los por uma licença MIT genérica. Antes de qualquer mirror, concluir a revisão dos demais arquivos compilados e das condições Windows aplicáveis.

## Entrega para integração e limites de encerramento

`exportpecl-notices.json` fornece **dois bundles / oito textos**: sete para o SHA256 MongoDB específico e um para o SHA256 memcached específico. Cada registro inclui nome sugerido, bytes, hash, URL primária, caminho dentro do fonte/SDK, descrição do escopo e caminho local da cópia exata. Os textos ficam em `exact-texts/<SHA256>.txt`. Não há texto genérico criado para substituir avisos upstream. A integração do instalador e seus testes pertencem ao root; este relatório não afirma que um suplemento recém-proposto já foi instalado ou validado em produção.

As provas principais são `primary-retrieval.json`, `*.static.json`, `*.entries.json`, `mongodb-source-byte-comparison.json`, `memcached-sdk-byte-comparison.json`, `imagick-sdk-byte-comparison.json` e `redis-inline-vendored-notices.json`. Os dois endereços exploratórios que retornaram 404 foram mantidos como indisponíveis, sem substituir silenciosamente versão/fonte. Houve um limite de caminho Windows na extração de evidência textual; a repetição usou nomes por hash e concluiu a inspeção, sem executar ou extrair payloads nativos para uso.

Pode-se encerrar a **recuperação dos avisos dedicados dos dois ZIPs PECL examinados**. A perda local só estará encerrada após a integração condicionada a esses hashes e testes de preservação. Permanece aberta a aprovação de **redistribuição completa** de Imagick/Redis e de builds PECL não examinados, bem como a correspondência fonte/binário e as obrigações específicas dos componentes reais. O resultado não altera as qualificações históricas da revisão anterior.
