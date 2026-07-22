# xmldsig2ed vendored test vectors

These files are the harness-consumed subset of the W3C Note **"Test Cases for
C14N 1.1 and XMLDSig Interoperability"** (Working Group Note, 10 June 2008).

- Note: https://www.w3.org/TR/xmldsig2ed-tests/
- Vector base URL: https://www.w3.org/TR/2008/NOTE-xmldsig2ed-tests-20080610/
- Retrieved: 2026-07-22
- License: W3C Document License (https://www.w3.org/Consortium/Legal/2015/doc-license).
  Redistributed here unmodified for interoperability testing.

There is no upstream archive; the ~40 vector directories are served directly
from the TR tree. The files are vendored (committed) here, and `w3cgen fetch
xmldsig2ed` overlays this tree into the gitignored `testdata/xmldsig2ed/`.

## What is vendored

Only the files the harness reads are vendored, not the full per-vendor interop
report tree (the upstream `-IAIK`/`-IBM`/`-SUN`/`-UPC`/`-ORCL` reference outputs,
`.digest`, `.derefURI`, `.digestinput`, `report.sh`, `Makefile`, `digest.xsl`).

- `c14n11/` — pure Canonical XML 1.1 cases: one `*-input.xml` per family and,
  per case, a `<case>.xpath` (node-set expression) and `<case>.output` (expected
  canonical bytes). The `<case>.output` here is the reference (vendor-neutral)
  output.
- `c14n11/appendixa/inputs.txt`, `outputs.txt` — RFC 3986 dot-segment removal
  pairs. Vendored for provenance only; helium's URI join is covered by the c14n
  suite, so these drive no test cases here.
- `xmldsig/defCan-{1,2,3}-signature.xml` — signed documents using C14N 1.1 as the
  canonicalization method (HMAC-SHA1, an XPath transform on the reference).
- `xmldsig/c14n11/xml-base-input.xml` — the external document that defCan-1's
  Reference (`c14n11/xml-base-input.xml`) points at. Vendored so the harness's
  FSReferenceResolver can serve it locally; the Reference URI joins against the
  signed doc's base to `xmldsig/c14n11/xml-base-input.xml` under the testdata root.
- `xmldsig/xpointer/xpointer-{1..6}-SUN.xml` (+ `input.xml`) — signed documents
  whose references use the XPointer framework (HMAC-SHA1, enveloped + C14N 1.1
  WithComments transforms).
- `xmldsig/dname/{diffRFCs-1..5,dnString-4,6,8}-SUN.xml` — X.509 Distinguished
  Name encoding cases (DSA-SHA1 signatures).
- `xmldsig/dname/certs/{Control,Equals,Escaped,John,Null,Number,Spacey,Trailing}.crt`
  and `keystore.p12` — the DName cases' certificate material. Their KeyInfo carries
  only an X509SubjectName (a DName string, the thing under test), so the harness
  selects the signing cert from these vendored certs by decoded-DName match.
  `keystore.p12` (PKCS#12, password `secret`) is the upstream private-key store;
  it is vendored for provenance only — verification needs only the public certs.

## Key material for the HMAC cases

The HMAC cases (defCan, xpointer) use the literal secret `secret` (the ASCII
bytes of the word), supplied directly in harness code, so no key material is
vendored for them.

## sha256

```
64aa45f8f421001580bb4f20adbb6433cd2f3b02945d6407b2638e7423e8dae5  c14n11/appendixa/inputs.txt
649d07a095d60859dbc8aa5649025007475e5528cc075c3eae9d4c2f12844f72  c14n11/appendixa/outputs.txt
57983d18acf4e308c5e9b7d51a62d994bba48cf56f304cb08e51d93829c0cba7  c14n11/xmlbase-c14n11spec-102.output
30c71f2486d38a4e1a9df5c10025db4ba8da678ffd2dc91ba92d0e15992bf10b  c14n11/xmlbase-c14n11spec-102.xpath
6bb89d395808257e5b558909abc95d1f60310d09613dcaa384bd9f3415923d61  c14n11/xmlbase-c14n11spec-input.xml
9d9b781ac135f3f8d4539722bc2fb952709c75c6dc493325bdbe8a6328552cf4  c14n11/xmlbase-c14n11spec2-102.output
30c71f2486d38a4e1a9df5c10025db4ba8da678ffd2dc91ba92d0e15992bf10b  c14n11/xmlbase-c14n11spec2-102.xpath
06d617b77d758ff98bc85c2b2b1c594d94bcf228a157317ee5605cc3e7d88403  c14n11/xmlbase-c14n11spec2-input.xml
5fbf2614047060e264722e6d0f8cdea28e37c7173e50a2649fafffe047cbbc13  c14n11/xmlbase-c14n11spec3-103.output
86039a6b1fc5336fd40b24d03e36272ff14957d56c422ed5ac1555c1679a820b  c14n11/xmlbase-c14n11spec3-103.xpath
b34ae1c7973a228d809547a1fd4d04b6c0656ced6a9fe1f1e5db2cabeb1d0053  c14n11/xmlbase-c14n11spec3-input.xml
ec6bfb952baae627d799f5416bfa8081c8576987f541d9d84e91f5b231c02ad0  c14n11/xmlbase-prop-1.output
02d0dbc384f59811671f41fff870e72139697c674b87e4c379e11d900860eee3  c14n11/xmlbase-prop-1.xpath
d3444a77421c27eef8dac7c5f9cf10bd61a6455d6015078b54512916802bde30  c14n11/xmlbase-prop-2.output
398631c6dba6a531dd452ea331283bd51529902f68de69dd0b113a9a51d68b51  c14n11/xmlbase-prop-2.xpath
5a057dda617d9648f0abee9a7724b027342fadb82efe0a8057250fcedf724272  c14n11/xmlbase-prop-3.output
f25e2f5365a2e5c6b3693c9a9b1db699046bb1da228f6d38e8629bf1803b001a  c14n11/xmlbase-prop-3.xpath
19a40f88c2a12929f77648de4104128e06325475f324743dde2a41cc55ac909f  c14n11/xmlbase-prop-4.output
3a2fa5c00f93b6e3790b13286f056c4dbb87b11b02fd0177ea4a1167ae2a90b2  c14n11/xmlbase-prop-4.xpath
c8379763986d0360b174cf79802ef7068a92fd4f00666e452a64f1ba7fd2d835  c14n11/xmlbase-prop-5.output
ab8a4021cb9f937b0ad640da4c447099f2d82e5eafe9c905dbccbf02bfad5908  c14n11/xmlbase-prop-5.xpath
202e308bf34b27351e0c6663d9b4b5a7c888e5dc8f1a7b79f2ba76e7d6cc9d3c  c14n11/xmlbase-prop-6.output
27a3335adffd6ca3d283d41e2bc409da353f535f086ed57f6bbf9dbbec01662c  c14n11/xmlbase-prop-6.xpath
b9ce9a6ea125f018978df4030743ff09e78d4ac2d696e269138c30adf1e86da0  c14n11/xmlbase-prop-7.output
9cbad48c4efe0fd2b2ad914eb1980bdcb8228654a658a126c39443eaeba5ee40  c14n11/xmlbase-prop-7.xpath
3afd91003897fdd5b0b7027fca405ad44831f61376615b807485df59c8850c67  c14n11/xmlbase-prop-input.xml
fbad769d9c191433a0d257c1b95d823de89a71fb687f2b2ce4a6ff77e14a7f90  c14n11/xmlid-1.output
398631c6dba6a531dd452ea331283bd51529902f68de69dd0b113a9a51d68b51  c14n11/xmlid-1.xpath
1d265c2a2f8a7fd41aa756bce11cd9aa1b5efef5754eae151bd8711026357ac6  c14n11/xmlid-2.output
1fd910df12ea8e920f3cf903a9c33f4c0d4ad3d9fbc7d72a21415a0caf6c496f  c14n11/xmlid-2.xpath
cfbc0445238247c8d1fd4e2fb78b907a5167a7000c49c3597aaa14d43e92b13e  c14n11/xmlid-input.xml
68dcedf2e59923f51a51080b9b8c9b40a2882f3b5732f92b15d878e222ad9e1c  c14n11/xmllang-1.output
014d0f72789edd6a6d62850aa87682078be25232268be864c61fdaa3637951c3  c14n11/xmllang-1.xpath
0ed213b79e0d4f258fc4e186797c2af676db1e399319baa0c8abd6002003d6fc  c14n11/xmllang-2.output
db2c4c52b5459f0ff365164027cbf302955cf7a453157db02fe844cfd0506d5a  c14n11/xmllang-2.xpath
8679f79b4bbfabd0a792d6eda306c332828ce079e275c0a020900890f9704007  c14n11/xmllang-3.output
1c6d5c832d17501706d7fecb8ad6aaad2c05f2691e512da66cd829d596aaa196  c14n11/xmllang-3.xpath
4e2fe27c0c85e0dd638d66b609dce1fdf0f29460ffe7f5f2f2b515d916ec6701  c14n11/xmllang-4.output
a0f280d03a453a8bb5c27b0e470fc9880bb1bd5651e4ceabcf461753a313b1b7  c14n11/xmllang-4.xpath
d4cbe3afac2974c35baf00d0c7f1fb88ec33ee910c63d4fa0f71a4fba8e4edb9  c14n11/xmllang-input.xml
23315e8d4519388e18904c027f099b2a9ecf7481579c6c1c0f6b97d3835593b5  c14n11/xmlspace-1.output
398631c6dba6a531dd452ea331283bd51529902f68de69dd0b113a9a51d68b51  c14n11/xmlspace-1.xpath
0ed213b79e0d4f258fc4e186797c2af676db1e399319baa0c8abd6002003d6fc  c14n11/xmlspace-2.output
940b5cd6fa1465311ed7888c94ee31d11d88ebca9c372eaf964be8663b9dd48f  c14n11/xmlspace-2.xpath
2a21bfe88d827cf6e3228537a60c17d005c2771bec363bd20ea7d8954a8d88d4  c14n11/xmlspace-3.output
f25e2f5365a2e5c6b3693c9a9b1db699046bb1da228f6d38e8629bf1803b001a  c14n11/xmlspace-3.xpath
bb446531182e6f5aac3235264b58fbd056697c1dfdcbe95a8e16c38864e4e730  c14n11/xmlspace-4.output
1fd910df12ea8e920f3cf903a9c33f4c0d4ad3d9fbc7d72a21415a0caf6c496f  c14n11/xmlspace-4.xpath
7ff19e389d7b9101a0920ce8bd081aac3bcfa6097fec54fa8a1ec6b1a6c62ab3  c14n11/xmlspace-input.xml
5892fdd7472cc906741e87eef05439566baa58664d45ef57aa5689c58b709087  xmldsig/defCan-1-signature.xml
3afd91003897fdd5b0b7027fca405ad44831f61376615b807485df59c8850c67  xmldsig/c14n11/xml-base-input.xml
e8044ff374c21ae5987ebfcceeb4f0a35584ac49d5db958c441e0748d5f7dc58  xmldsig/defCan-2-signature.xml
7f360649ceca6e12e360d21a648a496f37400e0e62ccb74bab55dcb745b99941  xmldsig/defCan-3-signature.xml
e7aee28ca753af06e2c61a2d6e75b081e017bce91c733a911ba70f7c147ea89a  xmldsig/dname/certs/Control.crt
4006145319f5f8ea87db6e2c431895171aa254d39c6849667c92ccbf1d26ad31  xmldsig/dname/certs/Equals.crt
15564f4d3ba1e4c9299e172c556156f4edc98a06ad102dbb548082c75585bcac  xmldsig/dname/certs/Escaped.crt
4d504d11f9797ebebfa7ffae60758048f5f4dea43c4807828a616bbf7c34965d  xmldsig/dname/certs/John.crt
65ea300ff59413e1233f79cfc5058655051890c89b3a4d070957157de86b0ae1  xmldsig/dname/certs/Null.crt
4a81358204b316243485809f2569f1d2eb403713d72be6a56b1bd96455311166  xmldsig/dname/certs/Number.crt
110636250b2e191d31b70c9e2eefed0768e4577cadc83ba7a83c20e91a809b0a  xmldsig/dname/certs/Spacey.crt
dd1402fbca0efd20af8a4554cc58fd7e9730f8765d83f073ae5e07cebee67db7  xmldsig/dname/certs/Trailing.crt
b056b464784825e467c21291febf375e8561b67847cdb0ea433049aad282dcfd  xmldsig/dname/certs/keystore.p12
f9c23e308fab3de0b8887b85f8ebe645ce78ef1c3f6b6ffe071c54ba17c9d78f  xmldsig/dname/diffRFCs-1-SUN.xml
77f578419019ccd9938068feaf7fad09909ebf59128a6f65e5546e0053b589de  xmldsig/dname/diffRFCs-2-SUN.xml
5100c4a30324f3d99094e971deb1fb332c0a1ef49fee351502e9f868e6f70f1e  xmldsig/dname/diffRFCs-3-SUN.xml
28653a660e1acdb6c710f260f1a1652190e5734493a5423709f0cb742fae9014  xmldsig/dname/diffRFCs-4-SUN.xml
73574309b798fc824d4212da4f775dd4f3b0382c68061db51e4c9573c6db3b91  xmldsig/dname/diffRFCs-5-SUN.xml
fa7d3dd68d8f0f6a9e9de5226f097fd1d87b7c34ead461d2d0d3716f67273e30  xmldsig/dname/dnString-4-SUN.xml
5e86e0996925e42777b410da2039c6e8a3a669d1f8aeaea7c52caec1d6f84d91  xmldsig/dname/dnString-6-SUN.xml
cace1c7d0ac4d426ddf29dbd340627561a8caf67ea53a48582b62abca2e7d574  xmldsig/dname/dnString-8-SUN.xml
44660e06c58f697564c5b960b82899ba25738cfe48da8cd057efcf76c22f7a29  xmldsig/xpointer/input.xml
4a2b74cd630acb46f31b193a078868269e715683fc6049fbf46ea1bcc6310880  xmldsig/xpointer/xpointer-1-SUN.xml
ff32da54d7bc8dfd3e6f1e9cd30b6c2cddb8aa874d362e2a0bcda8d2ca35f6ea  xmldsig/xpointer/xpointer-2-SUN.xml
93b1903fe0ee58a390b9d1e585abcd8f3f46b2abf014f47914e744ab36da9110  xmldsig/xpointer/xpointer-3-SUN.xml
8e7958730a8c2c53a935d2fb27f7186853fe03f25995b4fd296efde019aa253d  xmldsig/xpointer/xpointer-4-SUN.xml
0376be9706caaad3fb3f1eb286a50dba10bf3faf77b5e912fde510b08f5cb09f  xmldsig/xpointer/xpointer-5-SUN.xml
6a2297f663191616714400b4aaa076f5cce3f4b8dfad6b71f8b8bf75d6c085b1  xmldsig/xpointer/xpointer-6-SUN.xml
```
