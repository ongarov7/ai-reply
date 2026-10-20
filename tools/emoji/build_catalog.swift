// Builds the shared emoji catalog for the iOS and Android keyboards.
//
//   swift tools/emoji/build_catalog.swift          # validate and write both copies
//   swift tools/emoji/build_catalog.swift --check  # validate and compare, write nothing
//
// The emoji list below is curated by hand in Unicode CLDR / emoji-test order,
// default skin tone only, nothing newer than Emoji 15.1. Every run validates it
// against this Mac's Unicode data (Swift scalar properties, ICU RGI_Emoji set and
// CLDR emoji collation) and Apple Color Emoji (CoreText), and fails on any problem.
// See README.md in this folder.

import CoreText
import Foundation

// MARK: - Curated data
//
// Each category is a block of lines `<emoji version> <emoji> <emoji> ...`.
// `#` lines are subgroup labels (ignored). U+FE0F in the data is optional: the
// generator removes it and writes the fully-qualified form itself.
// `{keycaps}`, `{country-flags}` and `{subdivision-flags}` expand to generated lists.

let categories: [(id: String, data: String)] = [
("smileys_emotion", #"""
# face-smiling
1.0 😀
0.6 😃 😄 😁 😆 😅
3.0 🤣
0.6 😂
1.0 🙂 🙃
14.0 🫠
0.6 😉 😊
1.0 😇
# face-affection
11.0 🥰
0.6 😍
5.0 🤩
0.6 😘
1.0 😗
0.6 ☺️ 😚
1.0 😙
13.0 🥲
# face-tongue
0.6 😋
1.0 😛
0.6 😜
5.0 🤪
0.6 😝
1.0 🤑
# face-hand
1.0 🤗
5.0 🤭
14.0 🫢 🫣
5.0 🤫
1.0 🤔
14.0 🫡
# face-neutral-skeptical
1.0 🤐
5.0 🤨
0.7 😐
1.0 😑 😶
14.0 🫥
13.1 😶‍🌫️
0.6 😏 😒
1.0 🙄 😬
13.1 😮‍💨
3.0 🤥
15.0 🫨
15.1 🙂‍↔️ 🙂‍↕️
# face-sleepy
0.6 😌 😔 😪
3.0 🤤
1.0 😴
# face-unwell
0.6 😷
1.0 🤒 🤕
3.0 🤢
5.0 🤮
3.0 🤧
11.0 🥵 🥶 🥴
0.6 😵
13.1 😵‍💫
5.0 🤯
# face-hat
3.0 🤠
11.0 🥳
13.0 🥸
# face-glasses
1.0 😎 🤓
5.0 🧐
# face-concerned
1.0 😕
14.0 🫤
1.0 😟 🙁
0.7 ☹️
1.0 😮 😯
0.6 😲 😳
11.0 🥺
14.0 🥹
1.0 😦 😧
0.6 😨 😰 😥 😢 😭 😱 😖 😣 😞 😓 😩 😫
12.0 🥱
# face-negative
0.6 😤 😡 😠
5.0 🤬
1.0 😈
0.6 👿 💀
1.0 ☠️
# face-costume
0.6 💩
3.0 🤡
0.6 👹 👺 👻 👽 👾
1.0 🤖
# cat-face
0.6 😺 😸 😹 😻 😼 😽 🙀 😿 😾
# monkey-face
0.6 🙈 🙉 🙊
# heart
0.6 💌 💘 💝 💖 💗 💓 💞 💕 💟
1.0 ❣️
0.6 💔
13.1 ❤️‍🔥 ❤️‍🩹
0.6 ❤️
15.0 🩷
5.0 🧡
0.6 💛 💚 💙
15.0 🩵
0.6 💜
12.0 🤎
3.0 🖤
15.0 🩶
12.0 🤍
# emotion
0.6 💋 💯 💢 💥 💫 💦 💨
0.7 🕳️
0.6 💬
2.0 👁️‍🗨️ 🗨️
0.7 🗯️
1.0 💭
0.6 💤
"""#),
("people_body", #"""
# hand-fingers-open
0.6 👋
3.0 🤚
0.7 🖐️
0.6 ✋
1.0 🖖
14.0 🫱 🫲 🫳 🫴
15.0 🫷 🫸
# hand-fingers-partial
0.6 👌
13.0 🤌
12.0 🤏
0.6 ✌️
3.0 🤞
14.0 🫰
5.0 🤟
1.0 🤘
3.0 🤙
# hand-single-finger
0.6 👈 👉 👆
1.0 🖕
0.6 👇 ☝️
14.0 🫵
# hand-fingers-closed
0.6 👍 👎 ✊ 👊
3.0 🤛 🤜
# hands
0.6 👏 🙌
14.0 🫶
0.6 👐
5.0 🤲
3.0 🤝
0.6 🙏
# hand-prop
0.7 ✍️
0.6 💅
3.0 🤳
# body-parts
0.6 💪
12.0 🦾 🦿
11.0 🦵 🦶
0.6 👂
12.0 🦻
0.6 👃
5.0 🧠
13.0 🫀 🫁
11.0 🦷 🦴
0.6 👀
0.7 👁️
0.6 👅 👄
14.0 🫦
# person
0.6 👶
5.0 🧒
0.6 👦 👧
5.0 🧑
0.6 👱
12.1 🧑‍🦰 🧑‍🦱 🧑‍🦳 🧑‍🦲
0.6 👨
5.0 🧔
13.1 🧔‍♂️
4.0 👱‍♂️
11.0 👨‍🦰 👨‍🦱 👨‍🦳 👨‍🦲
0.6 👩
13.1 🧔‍♀️
4.0 👱‍♀️
11.0 👩‍🦰 👩‍🦱 👩‍🦳 👩‍🦲
5.0 🧓
0.6 👴 👵
# person-gesture
0.6 🙍
4.0 🙍‍♂️ 🙍‍♀️
0.6 🙎
4.0 🙎‍♂️ 🙎‍♀️
0.6 🙅
4.0 🙅‍♂️ 🙅‍♀️
0.6 🙆
4.0 🙆‍♂️ 🙆‍♀️
0.6 💁
4.0 💁‍♂️ 💁‍♀️
0.6 🙋
4.0 🙋‍♂️ 🙋‍♀️
12.0 🧏 🧏‍♂️ 🧏‍♀️
0.6 🙇
4.0 🙇‍♂️ 🙇‍♀️
3.0 🤦
4.0 🤦‍♂️ 🤦‍♀️
3.0 🤷
4.0 🤷‍♂️ 🤷‍♀️
# person-role
12.1 🧑‍⚕️
4.0 👨‍⚕️ 👩‍⚕️
12.1 🧑‍🎓
4.0 👨‍🎓 👩‍🎓
12.1 🧑‍🏫
4.0 👨‍🏫 👩‍🏫
12.1 🧑‍⚖️
4.0 👨‍⚖️ 👩‍⚖️
12.1 🧑‍🌾
4.0 👨‍🌾 👩‍🌾
12.1 🧑‍🍳
4.0 👨‍🍳 👩‍🍳
12.1 🧑‍🔧
4.0 👨‍🔧 👩‍🔧
12.1 🧑‍🏭
4.0 👨‍🏭 👩‍🏭
12.1 🧑‍💼
4.0 👨‍💼 👩‍💼
12.1 🧑‍🔬
4.0 👨‍🔬 👩‍🔬
12.1 🧑‍💻
4.0 👨‍💻 👩‍💻
12.1 🧑‍🎤
4.0 👨‍🎤 👩‍🎤
12.1 🧑‍🎨
4.0 👨‍🎨 👩‍🎨
12.1 🧑‍✈️
4.0 👨‍✈️ 👩‍✈️
12.1 🧑‍🚀
4.0 👨‍🚀 👩‍🚀
12.1 🧑‍🚒
4.0 👨‍🚒 👩‍🚒
0.6 👮
4.0 👮‍♂️ 👮‍♀️
0.7 🕵️
4.0 🕵️‍♂️ 🕵️‍♀️
0.6 💂
4.0 💂‍♂️ 💂‍♀️
13.0 🥷
0.6 👷
4.0 👷‍♂️ 👷‍♀️
14.0 🫅
3.0 🤴
0.6 👸 👳
4.0 👳‍♂️ 👳‍♀️
0.6 👲
5.0 🧕
3.0 🤵
13.0 🤵‍♂️ 🤵‍♀️
0.6 👰
13.0 👰‍♂️ 👰‍♀️
3.0 🤰
14.0 🫃 🫄
5.0 🤱
13.0 👩‍🍼 👨‍🍼 🧑‍🍼
# person-fantasy
0.6 👼 🎅
3.0 🤶
13.0 🧑‍🎄
11.0 🦸 🦸‍♂️ 🦸‍♀️ 🦹 🦹‍♂️ 🦹‍♀️
5.0 🧙 🧙‍♂️ 🧙‍♀️ 🧚 🧚‍♂️ 🧚‍♀️ 🧛 🧛‍♂️ 🧛‍♀️ 🧜 🧜‍♂️ 🧜‍♀️ 🧝 🧝‍♂️ 🧝‍♀️ 🧞 🧞‍♂️ 🧞‍♀️ 🧟 🧟‍♂️ 🧟‍♀️
14.0 🧌
# person-activity
0.6 💆
4.0 💆‍♂️ 💆‍♀️
0.6 💇
4.0 💇‍♂️ 💇‍♀️
0.6 🚶
4.0 🚶‍♂️ 🚶‍♀️
15.1 🚶‍➡️ 🚶‍♀️‍➡️ 🚶‍♂️‍➡️
12.0 🧍 🧍‍♂️ 🧍‍♀️ 🧎 🧎‍♂️ 🧎‍♀️
15.1 🧎‍➡️ 🧎‍♀️‍➡️ 🧎‍♂️‍➡️
12.1 🧑‍🦯
15.1 🧑‍🦯‍➡️
12.0 👨‍🦯
15.1 👨‍🦯‍➡️
12.0 👩‍🦯
15.1 👩‍🦯‍➡️
12.1 🧑‍🦼
15.1 🧑‍🦼‍➡️
12.0 👨‍🦼
15.1 👨‍🦼‍➡️
12.0 👩‍🦼
15.1 👩‍🦼‍➡️
12.1 🧑‍🦽
15.1 🧑‍🦽‍➡️
12.0 👨‍🦽
15.1 👨‍🦽‍➡️
12.0 👩‍🦽
15.1 👩‍🦽‍➡️
0.6 🏃
4.0 🏃‍♂️ 🏃‍♀️
15.1 🏃‍➡️ 🏃‍♀️‍➡️ 🏃‍♂️‍➡️
0.6 💃
3.0 🕺
0.7 🕴️
0.6 👯
4.0 👯‍♂️ 👯‍♀️
5.0 🧖 🧖‍♂️ 🧖‍♀️ 🧗 🧗‍♂️ 🧗‍♀️
# person-sport
3.0 🤺
1.0 🏇
0.7 ⛷️
0.6 🏂
0.7 🏌️
4.0 🏌️‍♂️ 🏌️‍♀️
0.6 🏄
4.0 🏄‍♂️ 🏄‍♀️
1.0 🚣
4.0 🚣‍♂️ 🚣‍♀️
0.6 🏊
4.0 🏊‍♂️ 🏊‍♀️
0.7 ⛹️
4.0 ⛹️‍♂️ ⛹️‍♀️
0.7 🏋️
4.0 🏋️‍♂️ 🏋️‍♀️
1.0 🚴
4.0 🚴‍♂️ 🚴‍♀️
1.0 🚵
4.0 🚵‍♂️ 🚵‍♀️
3.0 🤸
4.0 🤸‍♂️ 🤸‍♀️
3.0 🤼
4.0 🤼‍♂️ 🤼‍♀️
3.0 🤽
4.0 🤽‍♂️ 🤽‍♀️
3.0 🤾
4.0 🤾‍♂️ 🤾‍♀️
3.0 🤹
4.0 🤹‍♂️ 🤹‍♀️
# person-resting
5.0 🧘 🧘‍♂️ 🧘‍♀️
0.6 🛀
1.0 🛌
# family
12.0 🧑‍🤝‍🧑
1.0 👭
0.6 👫
1.0 👬
0.6 💏
2.0 👩‍❤️‍💋‍👨 👨‍❤️‍💋‍👨 👩‍❤️‍💋‍👩
0.6 💑
2.0 👩‍❤️‍👨 👨‍❤️‍👨 👩‍❤️‍👩
2.0 👨‍👩‍👦 👨‍👩‍👧 👨‍👩‍👧‍👦 👨‍👩‍👦‍👦 👨‍👩‍👧‍👧
2.0 👨‍👨‍👦 👨‍👨‍👧 👨‍👨‍👧‍👦 👨‍👨‍👦‍👦 👨‍👨‍👧‍👧
2.0 👩‍👩‍👦 👩‍👩‍👧 👩‍👩‍👧‍👦 👩‍👩‍👦‍👦 👩‍👩‍👧‍👧
4.0 👨‍👦 👨‍👦‍👦 👨‍👧 👨‍👧‍👦 👨‍👧‍👧
4.0 👩‍👦 👩‍👦‍👦 👩‍👧 👩‍👧‍👦 👩‍👧‍👧
# person-symbol
0.7 🗣️
0.6 👤
1.0 👥
13.0 🫂
0.6 👪
15.1 🧑‍🧑‍🧒 🧑‍🧑‍🧒‍🧒 🧑‍🧒 🧑‍🧒‍🧒
0.6 👣
"""#),
("animals_nature", #"""
# animal-mammal
0.6 🐵 🐒
3.0 🦍
12.0 🦧
0.6 🐶
0.7 🐕
12.0 🦮 🐕‍🦺
0.6 🐩 🐺
3.0 🦊
11.0 🦝
0.6 🐱
0.7 🐈
13.0 🐈‍⬛
1.0 🦁
0.6 🐯
1.0 🐅 🐆
0.6 🐴
15.0 🫎 🫏
0.6 🐎
1.0 🦄
5.0 🦓
3.0 🦌
13.0 🦬
0.6 🐮
1.0 🐂 🐃 🐄
0.6 🐷
1.0 🐖
0.6 🐗 🐽
1.0 🐏
0.6 🐑
1.0 🐐 🐪
0.6 🐫
11.0 🦙
5.0 🦒
0.6 🐘
13.0 🦣
3.0 🦏
11.0 🦛
0.6 🐭
1.0 🐁 🐀
0.6 🐹 🐰
1.0 🐇
0.7 🐿️
13.0 🦫
5.0 🦔
3.0 🦇
0.6 🐻
13.0 🐻‍❄️
0.6 🐨 🐼
12.0 🦥 🦦 🦨
11.0 🦘 🦡
0.6 🐾
# animal-bird
1.0 🦃
0.6 🐔
1.0 🐓
0.6 🐣 🐤 🐥 🐦 🐧
0.7 🕊️
3.0 🦅 🦆
11.0 🦢
3.0 🦉
13.0 🦤 🪶
12.0 🦩
11.0 🦚 🦜
15.0 🪽 🐦‍⬛ 🪿
15.1 🐦‍🔥
# animal-amphibian
0.6 🐸
# animal-reptile
1.0 🐊
0.6 🐢
3.0 🦎
0.6 🐍 🐲
1.0 🐉
5.0 🦕 🦖
# animal-marine
0.6 🐳
1.0 🐋
0.6 🐬
13.0 🦭
0.6 🐟 🐠 🐡
3.0 🦈
0.6 🐙 🐚
14.0 🪸
15.0 🪼
1.0 🦀
11.0 🦞
3.0 🦐 🦑
12.0 🦪
# animal-bug
0.6 🐌
3.0 🦋
0.6 🐛 🐜 🐝
13.0 🪲
0.6 🐞
5.0 🦗
13.0 🪳
0.7 🕷️ 🕸️
1.0 🦂
11.0 🦟
13.0 🪰 🪱
11.0 🦠
# plant-flower
0.6 💐 🌸 💮
14.0 🪷
0.7 🏵️
0.6 🌹
3.0 🥀
0.6 🌺 🌻 🌼 🌷
15.0 🪻
# plant-other
0.6 🌱
13.0 🪴
1.0 🌲 🌳
0.6 🌴 🌵 🌾 🌿
1.0 ☘️
0.6 🍀 🍁 🍂 🍃
14.0 🪹 🪺
0.6 🍄
"""#),
("food_drink", #"""
# food-fruit
0.6 🍇 🍈 🍉 🍊
1.0 🍋
15.1 🍋‍🟩
0.6 🍌 🍍
11.0 🥭
0.6 🍎 🍏
1.0 🍐
0.6 🍑 🍒 🍓
13.0 🫐
3.0 🥝
0.6 🍅
13.0 🫒
5.0 🥥
# food-vegetable
3.0 🥑
0.6 🍆
3.0 🥔 🥕
0.6 🌽
0.7 🌶️
13.0 🫑
3.0 🥒
11.0 🥬
5.0 🥦
12.0 🧄 🧅
3.0 🥜
14.0 🫘
0.6 🌰
15.0 🫚 🫛
15.1 🍄‍🟫
# food-prepared
0.6 🍞
3.0 🥐 🥖
13.0 🫓
5.0 🥨
11.0 🥯
3.0 🥞
12.0 🧇
1.0 🧀
0.6 🍖 🍗
5.0 🥩
3.0 🥓
0.6 🍔 🍟 🍕
1.0 🌭
5.0 🥪
1.0 🌮 🌯
13.0 🫔
3.0 🥙
12.0 🧆
3.0 🥚
0.6 🍳
3.0 🥘
0.6 🍲
13.0 🫕
5.0 🥣
3.0 🥗
1.0 🍿
12.0 🧈
11.0 🧂
5.0 🥫
# food-asian
0.6 🍱 🍘 🍙 🍚 🍛 🍜 🍝 🍠 🍢 🍣 🍤 🍥
11.0 🥮
0.6 🍡
5.0 🥟 🥠 🥡
# food-sweet
0.6 🍦 🍧 🍨 🍩 🍪 🎂 🍰
11.0 🧁
5.0 🥧
0.6 🍫 🍬 🍭 🍮 🍯
# drink
1.0 🍼
3.0 🥛
0.6 ☕
13.0 🫖
0.6 🍵 🍶
1.0 🍾
0.6 🍷 🍸 🍹 🍺 🍻
3.0 🥂 🥃
14.0 🫗
5.0 🥤
13.0 🧋
12.0 🧃 🧉 🧊
# dishware
5.0 🥢
0.7 🍽️
0.6 🍴
3.0 🥄
0.6 🔪
14.0 🫙
1.0 🏺
"""#),
("travel_places", #"""
# place-map
0.7 🌍 🌎
0.6 🌏
1.0 🌐
0.7 🗺️
0.6 🗾
11.0 🧭
# place-geographic
0.7 🏔️ ⛰️
0.6 🌋 🗻
0.7 🏕️ 🏖️ 🏜️ 🏝️ 🏞️
# place-building
0.7 🏟️ 🏛️ 🏗️
11.0 🧱
13.0 🪨 🪵 🛖
0.7 🏘️ 🏚️
0.6 🏠 🏡 🏢 🏣
1.0 🏤
0.6 🏥 🏦 🏨 🏩 🏪 🏫 🏬 🏭 🏯 🏰 💒 🗼 🗽
# place-religious
0.6 ⛪
1.0 🕌
12.0 🛕
1.0 🕍
0.7 ⛩️
1.0 🕋
# place-other
0.6 ⛲ ⛺ 🌁 🌃
0.7 🏙️
0.6 🌄 🌅 🌆 🌇 🌉 ♨️ 🎠
14.0 🛝
0.6 🎡 🎢 💈 🎪
# transport-ground
1.0 🚂
0.6 🚃 🚄 🚅
1.0 🚆
0.6 🚇
1.0 🚈
0.6 🚉
1.0 🚊 🚝 🚞 🚋
0.6 🚌
0.7 🚍
1.0 🚎 🚐
0.6 🚑 🚒 🚓
0.7 🚔
0.6 🚕
1.0 🚖
0.6 🚗
0.7 🚘
0.6 🚙
13.0 🛻
0.6 🚚
1.0 🚛 🚜
0.7 🏎️ 🏍️
3.0 🛵
12.0 🦽 🦼 🛺
0.6 🚲
3.0 🛴
11.0 🛹
13.0 🛼
0.6 🚏
0.7 🛣️ 🛤️ 🛢️
0.6 ⛽
14.0 🛞
0.6 🚨 🚥
1.0 🚦
3.0 🛑
0.6 🚧
# transport-water
0.6 ⚓
14.0 🛟
0.6 ⛵
3.0 🛶
0.6 🚤
0.7 🛳️ ⛴️ 🛥️
0.6 🚢
# transport-air
0.6 ✈️
0.7 🛩️
1.0 🛫 🛬
12.0 🪂
0.6 💺
1.0 🚁 🚟 🚠 🚡
0.7 🛰️
0.6 🚀
5.0 🛸
# hotel
0.7 🛎️
11.0 🧳
# time
0.6 ⌛ ⏳ ⌚ ⏰
1.0 ⏱️ ⏲️
0.7 🕰️
0.6 🕛
0.7 🕧
0.6 🕐
0.7 🕜
0.6 🕑
0.7 🕝
0.6 🕒
0.7 🕞
0.6 🕓
0.7 🕟
0.6 🕔
0.7 🕠
0.6 🕕
0.7 🕡
0.6 🕖
0.7 🕢
0.6 🕗
0.7 🕣
0.6 🕘
0.7 🕤
0.6 🕙
0.7 🕥
0.6 🕚
0.7 🕦
# sky & weather
0.6 🌑
1.0 🌒
0.6 🌓 🌔 🌕
1.0 🌖 🌗 🌘
0.6 🌙
1.0 🌚
0.6 🌛
0.7 🌜 🌡️
0.6 ☀️
1.0 🌝 🌞
12.0 🪐
0.6 ⭐ 🌟 🌠 🌌 ☁️ ⛅
0.7 ⛈️ 🌤️ 🌥️ 🌦️ 🌧️ 🌨️ 🌩️ 🌪️ 🌫️ 🌬️
0.6 🌀 🌈 🌂
0.7 ☂️
0.6 ☔
0.7 ⛱️
0.6 ⚡ ❄️
0.7 ☃️
0.6 ⛄
1.0 ☄️
0.6 🔥 💧 🌊
"""#),
("activities", #"""
# event
0.6 🎃 🎄 🎆 🎇
11.0 🧨
0.6 ✨ 🎈 🎉 🎊 🎋 🎍 🎎 🎏 🎐 🎑
11.0 🧧
0.6 🎀 🎁
0.7 🎗️ 🎟️
0.6 🎫
# award-medal
0.7 🎖️
0.6 🏆
1.0 🏅
3.0 🥇 🥈 🥉
# sport
0.6 ⚽ ⚾
11.0 🥎
0.6 🏀
1.0 🏐
0.6 🏈
1.0 🏉
0.6 🎾
11.0 🥏
0.6 🎳
1.0 🏏 🏑 🏒
11.0 🥍
1.0 🏓 🏸
3.0 🥊 🥋 🥅
0.6 ⛳
0.7 ⛸️
0.6 🎣
12.0 🤿
0.6 🎽 🎿
5.0 🛷 🥌
# game
0.6 🎯
12.0 🪀 🪁
0.6 🔫 🎱 🔮
13.0 🪄
0.6 🎮
0.7 🕹️
0.6 🎰 🎲
11.0 🧩 🧸
13.0 🪅
14.0 🪩
13.0 🪆
0.6 ♠️ ♥️ ♦️ ♣️
11.0 ♟️
0.6 🃏 🀄 🎴
# arts & crafts
0.6 🎭
0.7 🖼️
0.6 🎨
11.0 🧵
13.0 🪡
11.0 🧶
13.0 🪢
"""#),
("objects", #"""
# clothing
0.6 👓
0.7 🕶️
11.0 🥽 🥼
12.0 🦺
0.6 👔 👕 👖
5.0 🧣 🧤 🧥 🧦
0.6 👗 👘
12.0 🥻 🩱 🩲 🩳
0.6 👙 👚
15.0 🪭
0.6 👛 👜 👝
0.7 🛍️
0.6 🎒
13.0 🩴
0.6 👞 👟
11.0 🥾 🥿
0.6 👠 👡
12.0 🩰
0.6 👢
15.0 🪮
0.6 👑 👒 🎩 🎓
5.0 🧢
13.0 🪖
0.7 ⛑️
1.0 📿
0.6 💄 💍 💎
# sound
1.0 🔇
0.7 🔈
1.0 🔉
0.6 🔊 📢 📣
1.0 📯
0.6 🔔
1.0 🔕
# music
0.6 🎼 🎵 🎶
0.7 🎙️ 🎚️ 🎛️
0.6 🎤 🎧 📻
# musical-instrument
0.6 🎷 🎺
13.0 🪗
0.6 🎸 🎹 🎻
12.0 🪕
3.0 🥁
13.0 🪘
15.0 🪇 🪈
# phone
0.6 📱 📲 ☎️ 📞 📟 📠
# computer
0.6 🔋
14.0 🪫
0.6 🔌 💻
0.7 🖥️ 🖨️
1.0 ⌨️
0.7 🖱️ 🖲️
0.6 💽 💾 💿 📀
11.0 🧮
# light & video
0.6 🎥
0.7 🎞️ 📽️
0.6 🎬 📺 📷
1.0 📸
0.6 📹 📼 🔍 🔎
0.7 🕯️
0.6 💡 🔦 🏮
12.0 🪔
# book-paper
0.6 📔 📕 📖 📗 📘 📙 📚 📓 📒 📃 📜 📄 📰
0.7 🗞️
0.6 📑 🔖
0.7 🏷️
# money
13.0 🪙
0.6 💰 💴 💵
1.0 💶 💷
0.6 💸 💳
11.0 🧾
0.6 💹
# mail
0.6 ✉️ 📧 📨 📩 📤 📥 📦 📫 📪
0.7 📬 📭
0.6 📮
0.7 🗳️
# writing
0.6 ✏️ ✒️
0.7 🖋️ 🖊️ 🖌️ 🖍️
0.6 📝
# office
0.6 💼 📁 📂
0.7 🗂️
0.6 📅 📆
0.7 🗒️ 🗓️
0.6 📇 📈 📉 📊 📋 📌 📍 📎
0.7 🖇️
0.6 📏 📐 ✂️
0.7 🗃️ 🗄️ 🗑️
# lock
0.6 🔒 🔓 🔏 🔐 🔑
0.7 🗝️
# tool
0.6 🔨
12.0 🪓
0.7 ⛏️
1.0 ⚒️
0.7 🛠️ 🗡️
1.0 ⚔️
0.6 💣
13.0 🪃
1.0 🏹
0.7 🛡️
13.0 🪚
0.6 🔧
13.0 🪛
0.6 🔩
1.0 ⚙️
0.7 🗜️
1.0 ⚖️
12.0 🦯
0.6 🔗
15.1 ⛓️‍💥
0.7 ⛓️
13.0 🪝
11.0 🧰 🧲
13.0 🪜
# science
1.0 ⚗️
11.0 🧪 🧫 🧬
1.0 🔬 🔭
0.6 📡
# medical
0.6 💉
12.0 🩸
0.6 💊
12.0 🩹
14.0 🩼
12.0 🩺
14.0 🩻
# household
0.6 🚪
13.0 🛗 🪞 🪟
0.7 🛏️ 🛋️
12.0 🪑
0.6 🚽
13.0 🪠
1.0 🚿 🛁
13.0 🪤
12.0 🪒
11.0 🧴 🧷 🧹 🧺 🧻
13.0 🪣
11.0 🧼
14.0 🫧
13.0 🪥
11.0 🧽 🧯
3.0 🛒
# other-object
0.6 🚬
1.0 ⚰️
13.0 🪦
1.0 ⚱️
11.0 🧿
14.0 🪬
0.6 🗿
13.0 🪧
14.0 🪪
"""#),
("symbols", #"""
# transport-sign
0.6 🏧
1.0 🚮 🚰
0.6 ♿ 🚹 🚺 🚻 🚼 🚾
1.0 🛂 🛃 🛄 🛅
# warning
0.6 ⚠️
1.0 🚸
0.6 ⛔ 🚫
1.0 🚳
0.6 🚭
1.0 🚯 🚱 🚷 📵
0.6 🔞
1.0 ☢️ ☣️
# arrow
0.6 ⬆️ ↗️ ➡️ ↘️ ⬇️ ↙️ ⬅️ ↖️ ↕️ ↔️ ↩️ ↪️ ⤴️ ⤵️ 🔃
1.0 🔄
0.6 🔙 🔚 🔛 🔜 🔝
# religion
1.0 🛐 ⚛️
0.7 🕉️ ✡️ ☸️ ☯️ ✝️
1.0 ☦️
0.7 ☪️
1.0 ☮️ 🕎
0.6 🔯
15.0 🪯
# zodiac
0.6 ♈ ♉ ♊ ♋ ♌ ♍ ♎ ♏ ♐ ♑ ♒ ♓ ⛎
# av-symbol
1.0 🔀 🔁 🔂
0.6 ▶️ ⏩
0.7 ⏭️
1.0 ⏯️
0.6 ◀️ ⏪
0.7 ⏮️
0.6 🔼 ⏫ 🔽 ⏬
0.7 ⏸️ ⏹️ ⏺️
1.0 ⏏️
0.6 🎦
1.0 🔅 🔆
0.6 📶
15.0 🛜
0.6 📳 📴
# gender
4.0 ♀️ ♂️
13.0 ⚧️
# math
0.6 ✖️ ➕ ➖ ➗
14.0 🟰
11.0 ♾️
# punctuation
0.6 ‼️ ⁉️ ❓ ❔ ❕ ❗ 〰️
# currency
0.6 💱 💲
# other-symbol
4.0 ⚕️
0.6 ♻️
1.0 ⚜️
0.6 🔱 📛 🔰 ⭕ ✅ ☑️ ✔️ ❌ ❎ ➰
1.0 ➿
0.6 〽️ ✳️ ✴️ ❇️ ©️ ®️ ™️
# keycap
{keycaps}
0.6 🔟
# alphanum
0.6 🔠 🔡 🔢 🔣 🔤 🅰️ 🆎 🅱️ 🆑 🆒 🆓 ℹ️ 🆔 Ⓜ️ 🆕 🆖 🅾️ 🆗 🅿️ 🆘 🆙 🆚
0.6 🈁 🈂️ 🈷️ 🈶 🈯 🉐 🈹 🈚 🈲 🉑 🈸 🈴 🈳 ㊗️ ㊙️ 🈺 🈵
# geometric
0.6 🔴
12.0 🟠 🟡 🟢
0.6 🔵
12.0 🟣 🟤
0.6 ⚫ ⚪
12.0 🟥 🟧 🟨 🟩 🟦 🟪 🟫
0.6 ⬛ ⬜ ◼️ ◻️ ◾ ◽ ▪️ ▫️ 🔶 🔷 🔸 🔹 🔺 🔻 💠 🔘 🔳 🔲
"""#),
("flags", #"""
# flag
0.6 🏁 🚩 🎌
1.0 🏴
0.7 🏳️
4.0 🏳️‍🌈
13.0 🏳️‍⚧️
11.0 🏴‍☠️
# country-flag
{country-flags}
# subdivision-flag
{subdivision-flags}
"""#),
]

// RGI country flags (Emoji 15.1), alphabetical by region code as in CLDR.
let countryFlagCodes = """
AC AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ
BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ
CA CC CD CF CG CH CI CK CL CM CN CO CP CR CU CV CW CX CY CZ
DE DG DJ DK DM DO DZ
EA EC EE EG EH ER ES ET EU
FI FJ FK FM FO FR
GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY
HK HM HN HR HT HU
IC ID IE IL IM IN IO IQ IR IS IT
JE JM JO JP
KE KG KH KI KM KN KP KR KW KY KZ
LA LB LC LI LK LR LS LT LU LV LY
MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ
NA NC NE NF NG NI NL NO NP NR NU NZ
OM
PA PE PF PG PH PK PL PM PN PR PS PT PW PY
QA
RE RO RS RU RW
SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ
TA TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ
UA UG UM UN US UY UZ
VA VC VE VG VI VN VU
WF WS
XK
YE YT
ZA ZM ZW
""".split(whereSeparator: { $0 == " " || $0 == "\n" }).map(String.init)

/// The ten carrier flags of Emoji 0.6; the UN flag came in Emoji 4.0; all others in 2.0.
let countryFlagVersions: [String: String] = [
    "CN": "0.6", "DE": "0.6", "ES": "0.6", "FR": "0.6", "GB": "0.6",
    "IT": "0.6", "JP": "0.6", "KR": "0.6", "RU": "0.6", "US": "0.6",
    "UN": "4.0",
]
let subdivisionFlags = ["gbeng", "gbsct", "gbwls"]  // England, Scotland, Wales — Emoji 5.0

/// RGI sequences built only from old code points that are newer than Emoji 15.1,
/// so the completeness check below does not ask for them.
let newerThan15_1: [String: String] = [
    "\u{1F1E8}\u{1F1F6}": "E16.0 flag: Sark",
    "\u{1F9D1}\u{200D}\u{1FA70}": "E17.0 ballet dancer",
]

// MARK: - Constants and helpers

let maxVersion = Version("15.1")!
let knownVersions = ["0.6", "0.7", "1.0", "2.0", "3.0", "4.0", "5.0", "11.0", "12.0", "12.1",
                     "13.0", "13.1", "14.0", "15.0", "15.1"]
let zwj: UInt32 = 0x200D, vs16: UInt32 = 0xFE0F, keycapMark: UInt32 = 0x20E3
let blackFlag: UInt32 = 0x1F3F4, cancelTag: UInt32 = 0xE007F
let regionalIndicators: ClosedRange<UInt32> = 0x1F1E6...0x1F1FF
let tagCharacters: ClosedRange<UInt32> = 0xE0020...0xE007E
let skinTones: ClosedRange<UInt32> = 0x1F3FB...0x1F3FF
let hairComponents: ClosedRange<UInt32> = 0x1F9B0...0x1F9B3
let keycapBases: Set<UInt32> = Set("#*0123456789".unicodeScalars.map(\.value))
let genderSigns: Set<UInt32> = [0x2640, 0x2642]

struct Version: Comparable, CustomStringConvertible {
    let major: Int, minor: Int
    init(_ major: Int, _ minor: Int) { self.major = major; self.minor = minor }
    init?(_ text: String) {
        let parts = text.split(separator: ".")
        guard parts.count == 2, let a = Int(parts[0]), let b = Int(parts[1]) else { return nil }
        self.init(a, b)
    }
    static func < (l: Version, r: Version) -> Bool { (l.major, l.minor) < (r.major, r.minor) }
    var description: String { "\(major).\(minor)" }
}

func scalar(_ v: UInt32) -> Unicode.Scalar { Unicode.Scalar(v)! }
func string(_ values: [UInt32]) -> String {
    var s = String.UnicodeScalarView()
    values.forEach { s.append(scalar($0)) }
    return String(s)
}
func hex(_ s: String) -> String {
    s.unicodeScalars.map { String(format: "%04X", $0.value) }.joined(separator: " ")
}
func age(_ v: UInt32) -> Version? {
    guard let a = scalar(v).properties.age else { return nil }
    return Version(a.major, a.minor)
}
/// Lowest emoji version a code point of this Unicode age can have.
/// Unicode 6.0 and older → E0.6, 6.1 → E1.0, 7.0 → E0.7, 8.0 → E1.0, 9.0 → E3.0,
/// 10.0 → E5.0, from 11.0 on the emoji version equals the Unicode version.
func minimumEmojiVersion(forAge a: Version) -> Version {
    switch (a.major, a.minor) {
    case (0...5, _), (6, 0): return Version(0, 6)
    case (6, _): return Version(1, 0)
    case (7, _): return Version(0, 7)
    case (8, _): return Version(1, 0)
    case (9, _): return Version(3, 0)
    case (10, _): return Version(5, 0)
    default: return a
    }
}
func isTextDefault(_ v: UInt32) -> Bool {
    let p = scalar(v).properties
    return p.isEmoji && !p.isEmojiPresentation
}

var problems: [String] = []
func problem(_ text: String) { problems.append(text) }
func failIfProblems(_ stage: String) {
    guard !problems.isEmpty else { return }
    FileHandle.standardError.write("\n\(stage): \(problems.count) problem(s)\n".data(using: .utf8)!)
    for p in problems { FileHandle.standardError.write("  - \(p)\n".data(using: .utf8)!) }
    exit(1)
}

// MARK: - Structure and full qualification

enum Kind { case basic, keycap, flag, tag, zwj }

struct Entry {
    let category: String
    let source: String      // as written in the data
    let emoji: String       // fully-qualified form
    let version: String
    let kind: Kind
}

/// Classifies a sequence (with U+FE0F removed) and returns its fully-qualified form,
/// or nil after recording why the sequence is not a valid default-skin-tone emoji.
func qualify(_ raw: [UInt32], label: String) -> (String, Kind)? {
    guard !raw.isEmpty else { problem("\(label): empty token"); return nil }
    for v in raw {
        let s = scalar(v)
        if s.properties.generalCategory == .unassigned || s.properties.age == nil {
            problem("\(label): U+\(String(format: "%04X", v)) is unassigned"); return nil
        }
        if skinTones.contains(v) { problem("\(label): skin-tone modifier not allowed"); return nil }
    }
    // Flag: exactly two regional indicators.
    if raw.contains(where: regionalIndicators.contains) {
        guard raw.count == 2, raw.allSatisfy(regionalIndicators.contains) else {
            problem("\(label): regional indicators outside a two-letter flag"); return nil
        }
        return (string(raw), .flag)
    }
    // Tag sequence: black flag, tag letters, cancel tag.
    if raw.contains(where: { tagCharacters.contains($0) || $0 == cancelTag }) {
        guard raw.count >= 4, raw.first == blackFlag, raw.last == cancelTag,
              raw.dropFirst().dropLast().allSatisfy(tagCharacters.contains) else {
            problem("\(label): tag characters outside a black-flag tag sequence"); return nil
        }
        return (string(raw), .tag)
    }
    // Keycap: base, VS16, U+20E3.
    if raw.contains(keycapMark) || raw.contains(where: keycapBases.contains) {
        guard raw.count == 2, keycapBases.contains(raw[0]), raw[1] == keycapMark else {
            problem("\(label): keycap mark or keycap base outside a keycap sequence"); return nil
        }
        return (string([raw[0], vs16, keycapMark]), .keycap)
    }
    // Basic emoji or ZWJ sequence: elements of one emoji code point joined by U+200D.
    let elements = raw.split(separator: zwj, omittingEmptySubsequences: false)
    var out: [UInt32] = []
    for (i, element) in elements.enumerated() {
        guard element.count == 1, let v = element.first else {
            problem("\(label): element \(i + 1) is not a single code point (\(element.count))"); return nil
        }
        guard scalar(v).properties.isEmoji else {
            problem("\(label): U+\(String(format: "%04X", v)) is not an emoji code point"); return nil
        }
        if hairComponents.contains(v) && i == 0 {
            problem("\(label): hair component used on its own"); return nil
        }
        if i > 0 { out.append(zwj) }
        out.append(v)
        if isTextDefault(v) { out.append(vs16) }
    }
    return (string(out), elements.count > 1 ? .zwj : .basic)
}

/// Independent check of the written form: VS16 exactly after text-default code points,
/// U+200D only between elements, keycaps as base+VS16+U+20E3.
func isFullyQualified(_ s: String, kind: Kind) -> Bool {
    let v = s.unicodeScalars.map(\.value)
    switch kind {
    case .flag: return v.count == 2 && v.allSatisfy(regionalIndicators.contains)
    case .tag: return v.first == blackFlag && v.last == cancelTag && !v.contains(vs16)
    case .keycap: return v.count == 3 && keycapBases.contains(v[0]) && v[1] == vs16 && v[2] == keycapMark
    case .basic, .zwj:
        if v.first == zwj || v.last == zwj { return false }
        for (i, c) in v.enumerated() {
            let next = i + 1 < v.count ? v[i + 1] : nil
            if c == vs16 {
                guard i > 0, isTextDefault(v[i - 1]) else { return false }
            } else if c == zwj {
                guard next != zwj, next != vs16 else { return false }
            } else if isTextDefault(c) != (next == vs16) {
                return false
            }
        }
        return true
    }
}

// MARK: - Parse the data

var entries: [Entry] = []
var vs16Fixed = 0

func add(_ raw: String, version: String, category: String) {
    guard knownVersions.contains(version) else {
        problem("\(category): unknown emoji version \(version) for \(raw)"); return
    }
    let stripped = raw.unicodeScalars.map(\.value).filter { $0 != vs16 }
    guard let (emoji, kind) = qualify(stripped, label: "\(category) \(raw) [\(hex(raw))]") else { return }
    if emoji != raw { vs16Fixed += 1 }
    entries.append(Entry(category: category, source: raw, emoji: emoji, version: version, kind: kind))
}

func regionalFlag(_ code: String) -> String {
    string(code.unicodeScalars.map { 0x1F1E6 + $0.value - 0x41 })
}

for (id, data) in categories {
    for line in data.split(separator: "\n").map({ $0.trimmingCharacters(in: .whitespaces) }) {
        if line.isEmpty || line.hasPrefix("#") { continue }
        switch line {
        case "{keycaps}":
            for c in "#*0123456789".unicodeScalars {
                add(string([c.value, vs16, keycapMark]), version: c == "*" ? "2.0" : "0.6", category: id)
            }
        case "{country-flags}":
            for code in countryFlagCodes {
                add(regionalFlag(code), version: countryFlagVersions[code] ?? "2.0", category: id)
            }
        case "{subdivision-flags}":
            for tag in subdivisionFlags {
                add(string([blackFlag] + tag.unicodeScalars.map { 0xE0000 + $0.value } + [cancelTag]),
                    version: "5.0", category: id)
            }
        default:
            let tokens = line.split(separator: " ").map(String.init)
            guard tokens.count >= 2 else { problem("\(id): malformed line '\(line)'"); continue }
            for token in tokens.dropFirst() { add(token, version: tokens[0], category: id) }
        }
    }
}
failIfProblems("Structure")

// MARK: - Validation

// (1)+(2)+(3)+(5): one grapheme, fully qualified, unique, version rules.
var seen: [String: String] = [:]
for e in entries {
    let label = "\(e.category) \(e.emoji) [\(hex(e.emoji))]"
    if e.emoji.count != 1 { problem("\(label): \(e.emoji.count) grapheme clusters") }
    if !isFullyQualified(e.emoji, kind: e.kind) { problem("\(label): not fully qualified") }
    if let other = seen[e.emoji] { problem("\(label): duplicate (also in \(other))") }
    seen[e.emoji] = e.category

    let declared = Version(e.version)!
    if declared > maxVersion { problem("\(label): E\(e.version) is newer than \(maxVersion)") }
    let values = e.emoji.unicodeScalars.map(\.value)
    let ages = values.compactMap(age)
    if let newest = ages.max() {
        if newest > maxVersion { problem("\(label): uses a Unicode \(newest) code point") }
        let minimum = minimumEmojiVersion(forAge: newest)
        if declared < minimum { problem("\(label): E\(e.version) is older than its newest code point (Unicode \(newest) → E\(minimum))") }
    }
    if e.kind == .zwj && declared < Version(2, 0) { problem("\(label): ZWJ sequences start at E2.0") }
    if e.kind == .tag && declared < Version(5, 0) { problem("\(label): tag sequences start at E5.0") }
    if e.kind == .zwj && values.contains(where: genderSigns.contains) && declared < Version(4, 0) {
        problem("\(label): gendered ZWJ sequences start at E4.0")
    }
    if declared == Version(15, 1) && e.kind != .zwj { problem("\(label): Emoji 15.1 added only ZWJ sequences") }
}
failIfProblems("Validation")

// ICU (the system libicucore): RGI_Emoji membership, completeness, CLDR emoji collation order.
guard let icu = dlopen("/usr/lib/libicucore.A.dylib", RTLD_NOW) else {
    fatalError("cannot load libicucore")
}
func icuFunction<T>(_ name: String, as type: T.Type) -> T {
    guard let p = dlsym(icu, name) else { fatalError("ICU function \(name) not found") }
    return unsafeBitCast(p, to: type)
}
typealias USetOpenPattern = @convention(c) (UnsafePointer<UInt16>, Int32, UnsafeMutablePointer<Int32>) -> OpaquePointer?
typealias USetContainsString = @convention(c) (OpaquePointer?, UnsafePointer<UInt16>, Int32) -> Int8
typealias USetGetItemCount = @convention(c) (OpaquePointer?) -> Int32
typealias USetGetItem = @convention(c) (OpaquePointer?, Int32, UnsafeMutablePointer<Int32>, UnsafeMutablePointer<Int32>,
                                        UnsafeMutablePointer<UInt16>, Int32, UnsafeMutablePointer<Int32>) -> Int32
typealias UColOpen = @convention(c) (UnsafePointer<CChar>, UnsafeMutablePointer<Int32>) -> OpaquePointer?
typealias UColStrcoll = @convention(c) (OpaquePointer?, UnsafePointer<UInt16>, Int32, UnsafePointer<UInt16>, Int32) -> Int32
typealias UGetVersion = @convention(c) (UnsafeMutablePointer<UInt8>) -> Void

func icuVersion(_ name: String) -> String {
    var v = [UInt8](repeating: 0, count: 4)
    icuFunction(name, as: UGetVersion.self)(&v)
    return "\(v[0]).\(v[1])"
}
var status: Int32 = 0
let pattern = Array("[:RGI_Emoji:]".utf16)
let rgi = icuFunction("uset_openPattern", as: USetOpenPattern.self)(pattern, Int32(pattern.count), &status)
guard status <= 0, rgi != nil else { fatalError("ICU [:RGI_Emoji:] unavailable (status \(status))") }
let containsString = icuFunction("uset_containsString", as: USetContainsString.self)

for e in entries {
    let u = Array(e.emoji.utf16)
    if containsString(rgi, u, Int32(u.count)) == 0 { problem("\(e.category) \(e.emoji) [\(hex(e.emoji))]: not in ICU RGI_Emoji") }
}
failIfProblems("RGI check")

// Completeness: every RGI emoji without skin tones, made only of code points up to
// Unicode 15.1, must be in the catalog (or listed in `newerThan15_1`).
var rgiCandidates: [String] = []
let itemCount = icuFunction("uset_getItemCount", as: USetGetItemCount.self)(rgi)
let getItem = icuFunction("uset_getItem", as: USetGetItem.self)
var buffer = [UInt16](repeating: 0, count: 64)
for i in 0..<itemCount {
    var start: Int32 = 0, end: Int32 = 0, ec: Int32 = 0
    let length = getItem(rgi, i, &start, &end, &buffer, Int32(buffer.count), &ec)
    guard ec <= 0 else { fatalError("uset_getItem failed (\(ec))") }
    if length == 0 {
        for v in UInt32(start)...UInt32(end) { rgiCandidates.append(string([v])) }
    } else {
        rgiCandidates.append(String(decoding: buffer[0..<Int(length)], as: UTF16.self))
    }
}
for s in rgiCandidates {
    let v = s.unicodeScalars.map(\.value)
    if v.contains(where: skinTones.contains) { continue }
    if v.count == 1 && hairComponents.contains(v[0]) { continue }
    guard let newest = v.compactMap(age).max(), newest <= maxVersion else { continue }
    if seen[s] == nil && newerThan15_1[s] == nil { problem("missing RGI emoji \(s) [\(hex(s))]") }
}
for (s, why) in newerThan15_1 {
    if seen[s] != nil { problem("\(s) is listed as \(why) but is in the catalog") }
    if !rgiCandidates.contains(s) { problem("\(s) is listed as \(why) but is not RGI") }
}
failIfProblems("Completeness")

// Order: the whole catalog must ascend under the CLDR emoji collation (und-u-co-emoji).
let collator = icuFunction("ucol_open", as: UColOpen.self)("und-u-co-emoji", &status)
guard collator != nil, status <= 0 else { fatalError("ICU emoji collation unavailable (status \(status))") }
let strcoll = icuFunction("ucol_strcoll", as: UColStrcoll.self)
for (a, b) in zip(entries, entries.dropFirst()) {
    let x = Array(a.emoji.utf16), y = Array(b.emoji.utf16)
    if strcoll(collator, x, Int32(x.count), y, Int32(y.count)) >= 0 {
        problem("order: \(a.emoji) (\(a.category)) should not come before \(b.emoji) (\(b.category))")
    }
}
failIfProblems("Order check")

// (4) Apple Color Emoji: exactly one glyph, no fallback font, no .notdef.
// One exception, because Apple draws them that way: couple and kiss sequences
// (👩‍❤️‍👨, 👩‍❤️‍💋‍👨, …) come out as a left half with zero advance plus a right half,
// both at the same origin — one emoji cell. That form is accepted and listed.
let emojiFont = CTFontCreateWithName("AppleColorEmoji" as CFString, 48, nil)
guard CTFontCopyPostScriptName(emojiFont) as String == "AppleColorEmoji" else {
    fatalError("Apple Color Emoji is not installed")
}
struct Rendering { var glyphs: [CGGlyph] = []; var advances: [CGFloat] = []; var xs: [CGFloat] = []; var fonts: Set<String> = []; var width: Double = 0 }
func render(_ s: String) -> Rendering {
    let text = NSAttributedString(string: s,
                                  attributes: [NSAttributedString.Key(kCTFontAttributeName as String): emojiFont])
    let line = CTLineCreateWithAttributedString(text)
    var r = Rendering()
    r.width = CTLineGetTypographicBounds(line, nil, nil, nil)
    for run in CTLineGetGlyphRuns(line) as! [CTRun] {
        let n = CTRunGetGlyphCount(run)
        let range = CFRange(location: 0, length: n)
        var glyphs = [CGGlyph](repeating: 0, count: n)
        var advances = [CGSize](repeating: .zero, count: n)
        var positions = [CGPoint](repeating: .zero, count: n)
        CTRunGetGlyphs(run, range, &glyphs)
        CTRunGetAdvances(run, range, &advances)
        CTRunGetPositions(run, range, &positions)
        r.glyphs += glyphs
        r.advances += advances.map(\.width)
        r.xs += positions.map(\.x)
        let attrs = CTRunGetAttributes(run) as NSDictionary
        r.fonts.insert(CTFontCopyPostScriptName(attrs[kCTFontAttributeName as String] as! CTFont) as String)
    }
    return r
}
let cell = render("\u{1F600}").width
var halfPairs: [String] = []
for e in entries {
    let r = render(e.emoji)
    let label = "\(e.category) \(e.emoji) [\(hex(e.emoji))]"
    guard r.fonts == ["AppleColorEmoji"], !r.glyphs.contains(0) else {
        problem("\(label): drawn with \(r.fonts.sorted()) or missing glyph"); continue
    }
    if r.glyphs.count == 1 && r.width == cell { continue }
    let isHalfPair = e.kind == .zwj && r.glyphs.count == 2 && r.advances[0] == 0 &&
        r.xs.allSatisfy { $0 == 0 } && r.width == cell
    if isHalfPair { halfPairs.append(e.emoji); continue }
    problem("\(label): \(r.glyphs.count) glyph(s), width \(r.width) (one emoji is \(cell))")
}
failIfProblems("Apple Color Emoji")

// MARK: - Output

var output = ""
var counts: [(String, Int)] = []
for (id, _) in categories {
    output += "@\(id)\n"
    let items = entries.filter { $0.category == id }
    counts.append((id, items.count))
    for e in items { output += "\(e.emoji)\t\(e.version)\n" }
}
let bytes = output.data(using: .utf8)!

let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
let targets = ["AI-Reply/Shared/Keyboard/Emoji/emoji-catalog.txt",
               "AI-Reply-Android/app/src/main/assets/emoji/emoji-catalog.txt"].map { repo.appendingPathComponent($0) }
let checkOnly = CommandLine.arguments.contains("--check")
for url in targets {
    if checkOnly {
        if (try? Data(contentsOf: url)) != bytes { problem("\(url.path) is out of date") }
    } else {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try bytes.write(to: url)
    }
}

let maxDeclared = entries.map { Version($0.version)! }.max()!
print("ICU \(icuVersion("u_getVersion")), Unicode \(icuVersion("u_getUnicodeVersion")); RGI items checked: \(rgiCandidates.count)")
for (id, n) in counts { print(id.padding(toLength: 16, withPad: " ", startingAt: 0) + String(format: "%5d", n)) }
print("total".padding(toLength: 16, withPad: " ", startingAt: 0) + String(format: "%5d", entries.count) +
      "   max version \(maxDeclared)   VS16 added: \(vs16Fixed)")
print("drawn as two halves in one cell by Apple Color Emoji: \(halfPairs.joined(separator: " "))")
failIfProblems("Check")
print(checkOnly ? "up to date" : "wrote \(targets.count) files (\(bytes.count) bytes each)")
