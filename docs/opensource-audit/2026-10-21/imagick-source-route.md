# Imagick: fontes, receita Windows e possibilidade de recomposição

Revisão estática de 21/10/2026. Há uma rota histórica concreta para reconstruir candidatos ao SDK usado pelo Imagick, incluindo o patch PHP/Windows e as árvores modificadas pelo ImageMagick. A correspondência integral entre essas fontes e os binários ainda não está demonstrada. Os avisos suplementares de licença não encerram essa pendência.

O recorte é o ZIP PECL Imagick 3.7.0 para PHP 8.3, SHA-256 `4204071decbb06dce9f212a63ace0aa0c1b48fd344176b8274cd7d32ab674c71`, e o [SDK oficial ImageMagick 7.1.0-18 vc15 x64](https://windows.php.net/downloads/pecl/deps/ImageMagick-7.1.0-18-vc15-x64.zip), SHA-256 `3684a58b0896e2a55995029fa92cc13bd1ac778e03cdf8682c4369bbef86be9e`. A comparação anterior demonstrou os mesmos bytes nas **166 DLLs companheiras**: 28 `CORE_RL`, 134 `IM_MOD_RL`, `FILTER_analyze`, `ImageMagickObject`, `X11` e `Xext`. A extensão `php_imagick.dll` é separada dessa contagem. Consulte também [a análise dos companions](runtime-companions-followup.md) e [os termos e avisos por componente](imagick-license-closure.md).

Nenhum binário ou script baixado foi executado, nenhum runtime foi iniciado e nenhum arquivo foi publicado nesta frente. ZIPs/TGZs foram tratados como dados. A investigação preservou respostas, URLs, datas, tamanhos e hashes em `build/verification/redis-imagick-licenses-20261021/imagick-sources/`; os downloads grandes permanecem ignorados pelo Git.

## Rota primária encontrada

1. **Extensão PHP.** O [pacote fonte oficial Imagick 3.7.0](https://pecl.php.net/get/imagick-3.7.0.tgz), 360.138 bytes, foi preservado com SHA-256 `5a364354109029d224bcbb2e82e15b248be9b641227f45e63425c06531792d3e`. Ele é fonte conhecida da versão, sem prova de que esse TGZ e uma receita integralmente registrada produziram o ZIP Windows examinado.
2. **Receita PHP/Windows.** O [commit `6db9aa33a4ddccbedec609dcf8fad8e90275bffa` do winlib-builder](https://github.com/winlibs/winlib-builder/commit/6db9aa33a4ddccbedec609dcf8fad8e90275bffa), de 14/12/2021 às 12:15:46 UTC, altera precisamente a receita ImageMagick. O [workflow desse commit](https://github.com/winlibs/winlib-builder/blob/6db9aa33a4ddccbedec609dcf8fad8e90275bffa/.github/workflows/imagemagick.yml) usa `windows-2019`, o repositório ImageMagick-Windows em `main`, `CloneRepositories.cmd`, `VisualDynamicMT.sln`, plataforma x64, toolset v141 para vc15 e Windows SDK 10.0.18362.0. Configura `/noHdri /noOpenCL /deprecated /VS2017`. Esses parâmetros concordam com os metadados do SDK; o workflow encontrado não é um log autenticado da execução que gerou o arquivo.
3. **Patch necessário.** A mesma árvore contém [patches/imagemagick.patch](https://github.com/winlibs/winlib-builder/blob/6db9aa33a4ddccbedec609dcf8fad8e90275bffa/patches/imagemagick.patch): reconhece `/noOpenCL` e desativa a emissão de `TreatWarningAsError`. O patch foi preservado no ZIP completo da receita, SHA-256 `198822d70372225fc5f6cd0502402c760858b0a4a1dba4d7f6fb7879090357da`.
4. **Clonagem e configuração dos delegates.** A árvore histórica [ImageMagick-Windows `66bcf66256c836c667d6d8b30fd70ef147e0213a`](https://github.com/ImageMagick/ImageMagick-Windows/tree/66bcf66256c836c667d6d8b30fd70ef147e0213a) contém [CloneRepositories.sh](https://github.com/ImageMagick/ImageMagick-Windows/blob/66bcf66256c836c667d6d8b30fd70ef147e0213a/CloneRepositories.sh), que clona os forks da organização ImageMagick em `main`. A receita não fixa commits individuais. O [VisualMagick candidato `c90455a633a5f72cdf14b3f4f15a380474cb8a8f`](https://github.com/ImageMagick/VisualMagick/tree/c90455a633a5f72cdf14b3f4f15a380474cb8a8f) contém `Config.txt` por biblioteca, incluindo fontes, exclusões, defines, dependências e tipo de vínculo.

O índice oficial PHP data o SDK de 14/12/2021 às 13:50 e `bin/configure.xml` registra `RELEASE_DATE=2021-12-14`, VS2017 e x64. Foram escolhidos candidatos pela última alteração visível no histórico atual de `main` até esse instante. Isso recupera modificações Windows reais em vez de presumir que uma tag upstream com a mesma versão é a fonte exata. **O horário do índice e um cutoff de histórico não provam quais commits foram efetivamente usados.**

## Corroborações específicas e seus limites

**Núcleo ImageMagick.** Os 125 cabeçalhos públicos presentes em `include/MagickCore` e `include/MagickWand` coincidem após normalização CRLF/LF com [ImageMagick `bd62b31c436a37f85458fc149b4867486ce147b2`](https://github.com/ImageMagick/ImageMagick/tree/bd62b31c436a37f85458fc149b4867486ce147b2), de 14/12/2021. Os bytes brutos diferem pelas terminações de linha. A comparação inclui `version.h`; não foi feita equivalência entre todo o código compilado e a DLL. A tag lançada posteriormente `7.1.0-18` não foi promovida automaticamente a fonte exata do SDK de 14/12.

**LibRaw.** A divergência do DLL é reproduzida dentro de uma única árvore candidata: [ImageMagick/libraw `944d5ed7758f04a6b11419b510dabc3d206f58e4`](https://github.com/ImageMagick/libraw/tree/944d5ed7758f04a6b11419b510dabc3d206f58e4) contém `ImageMagick/ImageMagick.version.h` com recurso `0.20.0.1 (2021-02-08)` e `libraw/libraw_version.h` com `0.21.0-Snapshot202101`. Esses dois valores concordam respectivamente com o recurso PE e a string do DLL. Portanto, escolher apenas a tag LibRaw 0.20 ou uma tag 0.21 seria insuficiente.

**Pango.** O fork [ImageMagick/pango `30df8db265a687ba463ca1f42b25ba924994c905`](https://github.com/ImageMagick/pango/tree/30df8db265a687ba463ca1f42b25ba924994c905) declara 1.45.3 em `meson.build` e no recurso ImageMagick, mas preserva 1.45.2 nos cabeçalhos `pango/config.h` e `pango/pango-features.h`. `pango-utils.c` devolve esse último macro. Isso explica a combinação observada de recurso 1.45.3 com string 1.45.2 dentro da árvore Windows candidata, sem certificar todo o build.

**PDBs.** Os 28 arquivos `bin/CORE_RL_*.pdb` do SDK foram lidos como MSF: todos têm o stream DBI vazio, sem registros C13 de checksum de fontes. Nas DLLs correspondentes não foi encontrada assinatura `RSDS`. Esses PDBs não forneceram uma ponte adicional de identidade/checksums entre as fontes candidatas e o código final. A leitura estrutural segue a [documentação MSF](https://llvm.org/docs/PDB/MsfFile.html) e a [documentação DBI](https://llvm.org/docs/PDB/DbiStream.html) do LLVM.

## Matriz das 28 bibliotecas e quatro auxiliares

Os números abaixo são os recursos PE; nas três bibliotecas Magick eles expressam a ABI, não a versão comercial do ImageMagick. “Receita, sem snapshot” significa que o fork é identificado nominalmente no script histórico e possui configuração concreta, mas um arquivo imutável desse fork não foi capturado nesta frente. Todas as árvores capturadas continuam candidatas, sem reprodução do build.

| Família `CORE_RL` | Recurso PE | Fonte candidata preservada / rota histórica | Observação de vínculo |
| --- | --- | --- | --- |
| bzlib | 1.0.8 | `ImageMagick/bzlib`, receita, sem snapshot | DLL |
| cairo | 1.16.0 | `ImageMagick/cairo` `d3761a8c73a2` | DLL; pixman estático |
| exr | 3.1.3 | `ImageMagick/exr` `bbb5fbd23ada`¹ | DLL; incorpora árvores Imath/OpenEXR |
| flif | 0.3.0 | `ImageMagick/flif` `8754df24579c` | DLL, `HAS_ENCODER` |
| freetype | 2.10.4 | `ImageMagick/freetype` `a18906091cd1` | DLL |
| fribidi | 1.0.10 | `ImageMagick/fribidi` `bbdbbfe99140` | DLL, importada por Pango |
| glib | 2.64.3 | `ImageMagick/glib` `451260500e80` | DLL; ffi estática |
| harfbuzz | 2.8.1 | `ImageMagick/harfbuzz` `0a25647d4a5f` | DLL, depende de FreeType/GLib |
| jp2 (JasPer) | 2.0.16 | `ImageMagick/jp2`, receita, sem snapshot | DLL; distinta de OpenJPEG |
| jpeg-turbo | 2.1.1 | `ImageMagick/jpeg-turbo`, receita, sem snapshot | DLL |
| jpeg-xl | 0.6.0 | `ImageMagick/jpeg-xl`, receita, sem snapshot | DLL; brotli/highway estáticos |
| lcms | 2.12.0 | `ImageMagick/lcms`, receita, sem snapshot | DLL |
| libheif | 1.12.0 | `ImageMagick/libheif` `e106504e3bf3` | DLL; aom/libde265 estáticos |
| liblzma | 5.2.5 | `ImageMagick/liblzma`, receita, sem snapshot | DLL |
| libraw | 0.20.0.1 | `ImageMagick/libraw` `944d5ed7758f` | DLL; fonte também declara snapshot 0.21 |
| librsvg | 2.40.20 | `ImageMagick/librsvg` `dbea2156a5a5` | DLL; croco e fontes gdk-pixbuf incorporadas |
| libxml | 2.9.10 | `ImageMagick/libxml`, receita, sem snapshot | DLL |
| libzip | 1.7.3 | `ImageMagick/libzip`, receita, sem snapshot | DLL |
| lqr | 0.4.2 | `ImageMagick/lqr` `4881b63c9786` | DLL, importa GLib |
| Magick++ | ABI 5.0.0 | `ImageMagick/ImageMagick` `bd62b31c436a` | DLL |
| MagickCore | ABI 10.0.0 | mesma árvore | DLL; raqm estática |
| MagickWand | ABI 10.0.0 | mesma árvore | DLL |
| openjpeg | 2.3.1 | `ImageMagick/openjpeg`, receita, sem snapshot | DLL |
| pango | 1.45.3 | `ImageMagick/pango` `30df8db265a6` | DLL; string 1.45.2 explicada acima |
| png | 1.6.37 | `ImageMagick/png`, receita, sem snapshot | DLL |
| tiff | 4.3.0 | `ImageMagick/tiff`, receita, sem snapshot | DLL; configuração cita JBIG, sujeita ao filtro de licença |
| webp | 1.2.0 | `ImageMagick/webp`, receita, sem snapshot | DLL |
| zlib | 1.2.11 | `ImageMagick/zlib`, receita, sem snapshot | DLL |

¹ O snapshot exr e os quatro snapshots estáticos adicionais foram preservados pela frente de termos em `imagick-notices/nested-archive-retrieval.json`. Não são inferidos de uma tag upstream.

| Auxiliar | Origem identificada | Limite |
| --- | --- | --- |
| `FILTER_analyze.dll` | fontes `ImageMagick/filters`, configuração `VisualMagick/filters/Config.txt` | sem reprodução do binário |
| `ImageMagickObject.dll` | `contrib/win32/ATL7/ImageMagickObject`, commit candidato contrib `2b0f77d1c4b0` | sem reprodução do binário |
| `X11.dll` | entrada **pré-compilada** `VisualMagick/bin/X11.dll`, commit `c90455a633a5` | 729.088 bytes, SHA-256 `8c26da60f5f8e3b7e3f945784973cd7e4720c7c56b8dc753e8776eb5c1f7b8a4`, idêntica ao SDK; fonte correspondente/versão não estabelecidas |
| `Xext.dll` | entrada **pré-compilada** `VisualMagick/bin/Xext.dll`, mesmo commit | 53.248 bytes, SHA-256 `3b703dbc035db435aa109a143376a9b6cc3d9dff89a75af116d4bec8938bf4e8`, idêntica ao SDK; fonte correspondente/versão não estabelecidas |

Os 134 módulos `IM_MOD_RL` são tratados pelos diretórios `coders` da mesma receita e suas dependências de delegates, sem inventar 134 versões independentes. O nome do módulo sozinho não prova inclusão de uma biblioteca opcional. Em particular, `VisualMagick/jbig/Config.txt` marca `[INCOMPATIBLE_LICENSE]`, `TargetPage.cpp` inicia a opção correspondente desabilitada e `Project.cpp` filtra esse projeto. O workflow PHP encontrado não habilita essa opção. Isso é consistente com a ausência de `CORE_RL_jbig_.dll` no ZIP; não permite atribuir GPL ao DLL de módulo JBIG apenas pelo nome.

## Dependências estáticas dentro de DLLs

Os oito projetos a seguir possuem `[STATIC]` na receita histórica. Seus `.lib` do SDK contêm objetos COFF, em vez de apenas membros de importação. Isso, os imports PE das DLLs e as strings identificadas pela frente de termos dão suporte à decomposição abaixo. Uma lista `[DEPENDENCIES]` isolada não demonstra que todos os objetos foram retidos pelo linker.

| Projeto estático | Objetos COFF no `.lib` | Consumidor indicado pela receita | Fonte candidata preservada |
| --- | ---: | --- | --- |
| aom | 165 | libheif; diretórios av1 decoder/encoder, fastfeat e vector | `6c5b8bb6b845`¹ |
| brotli | 29 | jpeg-xl | `94ead6d89dae`¹ |
| croco | 26 | librsvg | `31d07566601c` |
| ffi | 8 | glib | `18a302fef390` |
| highway | 3 | jpeg-xl | `a5b6622c32b0`¹ |
| libde265 | 61 | libheif; `HAVE_LIBDE265`/`LIBDE265_STATIC_BUILD` | `58ccddd4a8e5` |
| pixman | 32 | cairo; também listado por Pango | `a4e44a8ce76d` |
| raqm | 1 | MagickCore | `90ad2f543cd8`¹ |

O arquivo `VisualMagick/libheif/Config.txt` inclui AOM decoder/encoder, inclui libde265 e exclui explicitamente `heif_encoder_x265.cc`. O arquivo `librsvg/Config.txt` incorpora diretórios de gdk-pixbuf dentro da própria DLL. O arquivo `exr/Config.txt` incorpora as árvores Imath e OpenEXR; os respectivos avisos precisam acompanhar essa composição. Não se deve reduzir a análise a uma lista de DLLs visíveis.

No FLIF, `Config.txt` define `HAS_ENCODER`, e a inspeção independente encontrou exports de encoder e decoder. A entrada `[LICENSE]` da receita aponta apenas `LICENSE_Apache2`; ela é insuficiente como inventário legal do build com encoder. A árvore fonte preservada contém os termos aplicáveis ao encoder e a frente de termos trata LGPLv3+/GPLv3, além dos textos permissivos pertinentes. Esse caso demonstra por que `NOTICE` e a seleção histórica de arquivos de licença não constituem liberação integral do SDK.

## Fontes e recomposição ainda necessárias

Para os componentes cuja distribuição segue LGPL, é necessário preservar a licença e os direitos efetivos de modificação e depuração. Há opções de vínculo dinâmico que permitem o uso de uma versão modificada e compatível da biblioteca; quando essa opção não se aplica, a rota depende de fontes e materiais que permitam recompor/relinkar o trabalho. A [LGPL 2.1, seção 6](https://www.gnu.org/licenses/old-licenses/lgpl-2.1.txt) e a [LGPL 3, seção 4](https://www.gnu.org/licenses/lgpl-3.0.txt) distinguem essas alternativas. A distribuição da própria biblioteca continua sujeita às condições de fontes pertinentes, inclusive as alterações utilizadas; a condição específica precisa seguir o texto efetivamente escolhido para cada componente.

Aqui há DLLs separadas para GLib, FriBidi, Cairo, Pango, librsvg, lqr, LibRaw, libheif e FLIF, mas **croco e libde265 entram estaticamente em DLLs consumidoras**. Para esses componentes, substituir apenas um arquivo de DLL com o nome da biblioteca estática não é uma rota disponível. É preciso uma rota funcional para reconstruir a DLL consumidora com a biblioteca modificada e os insumos necessários. Os `.lib` presentes no SDK não demonstram, por si, que existe um conjunto completo e adequado de fontes/objetos/scripts para essa recomposição. FreeType, HarfBuzz e as opções alternativas de licença de outros componentes devem seguir seus termos próprios; não foram classificados todos como LGPL por pertencerem ao mesmo SDK.

O catálogo solicita download direto do arquivo oficial PHP; esta investigação não transforma essa rota em um mirror do LaraEnv nem publica o SDK. Para encerrar a pendência de fontes e recomposição identificada no artefato examinado, falta estabelecer um conjunto verificável que una:

1. os commits realmente usados, todas as alterações locais e as fontes aninhadas incorporadas;
2. a receita completa do SDK e da extensão PHP, os parâmetros relevantes e a versão dos insumos de build;
3. a forma de distribuição das fontes/materiais exigida pelas opções de licença escolhidas e uma rota efetiva de substituição ou recomposição compatível;
4. evidência suficiente de que são as fontes corretas do artefato examinado, por registros originais ou outra correlação verificável; reprodução bit a bit não é um requisito geral imposto por este levantamento;
5. a origem e os termos exatos de X11/Xext, hoje rastreados apenas até cópias pré-compiladas do repositório histórico.

Nenhuma dessas etapas exige publicar o backend privado. A documentação de fontes e a recomposição discutidas aqui dizem respeito às bibliotecas e aos trabalhos que efetivamente se vinculam a elas; este levantamento estático não caracteriza automaticamente o backend como parte desses trabalhos.

## Evidências reproduzíveis

Todos os caminhos abaixo são relativos à raiz do repositório:

- `build/verification/redis-imagick-licenses-20261021/imagick-sources/discovery-results.json`, `recipes-results.json`, `history-results.json`, `winbuild-results.json`: consultas primárias, receita histórica e tentativas de arquivos oficiais.
- `imagick-sources/candidate-commits.json`, `sources-results.json`: 19 árvores candidatas completas, cada uma com commit, URL, bytes e SHA-256; `archives/` mantém os ZIPs/TGZs inertes.
- `imagick-sources/source-texts.json`: paths, URLs imutáveis e hashes brutos dos arquivos de configuração e das provas de versão extraídos.
- `imagick-sources/source-route-analysis.json`: matriz completa, dependências, inventário dos `.lib`, hashes e comparação individual dos 125 cabeçalhos.
- `imagick-sources/pdb-source-correlations.json`: leitura dos 28 PDBs e o resultado negativo da busca de ponte DBI/RSDS.
- `imagick-sources/webhistory-results.json` e `primary-terms/`: cópias oficiais GNU e resultados de recuperação; falhas HTTP/rede não são tomadas como prova de indisponibilidade universal da fonte.
- `build/verification/runtime-publication-20261021/companions/imagick-sdk-byte-comparison.json`: comparação dos 166 companions; `pecl-imagick-3.7.0-8.3.static.json`: metadados PE e imports lidos anteriormente.
- `build/verification/redis-imagick-licenses-20261021/imagick-notices/nested-archive-retrieval.json`: as cinco árvores adicionais da frente de termos, com URLs/commits e hashes.

`analyze_sources.py` e `inspect_pdb.py` refazem apenas leituras estáticas desses dados locais. Os avisos, a disponibilidade de fontes candidatas e a identidade binária são evidências distintas e permanecem registrados separadamente.
