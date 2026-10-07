"""The fonts a cover's text is matched against: the display faces Chinese
covers are actually set in, plus a few Latin ones. `family` / `weight` are what
davinci's style takes; `file` / `index` let PIL draw the same glyphs."""
import os

H = os.path.expanduser('~/Library/Fonts')
S = '/System/Library/Fonts'

FONTS = [
    # key, family, weight, file, ttc index, scripts it is worth trying for
    ('youshe', 'YouSheBiaoTiHei', 400, f'{H}/优设标题黑.ttf', 0, 'cjk latin'),
    ('shuhei', 'Alimama ShuHeiTi', 700, f'{H}/AlimamaShuHeiTi-Bold.otf', 0, 'cjk latin'),
    ('pangmen', '庞门正道标题体免费版', 400, f'{H}/PangMenZhengDaoBiaoTiTiMianFeiBan-2.ttf', 0, 'cjk latin'),
    ('huangyou', 'zcoolqingkehuangyouti', 400, f'{H}/ZhanKuQingKeHuangYouTi-2.ttf', 0, 'cjk latin'),
    ('smiley', 'Smiley Sans', 400, f'{H}/SmileySans-Oblique.otf', 0, 'cjk latin'),
    ('harmony900', 'HarmonyOS Sans SC', 900, f'{H}/HarmonyOS_Sans_SC_Black.ttf', 0, 'cjk latin'),
    ('harmony700', 'HarmonyOS Sans SC', 700, f'{H}/HarmonyOS_Sans_SC_Bold.ttf', 0, 'cjk latin'),
    ('harmony500', 'HarmonyOS Sans SC', 500, f'{H}/HarmonyOS_Sans_SC_Medium.ttf', 0, 'cjk latin'),
    ('harmony400', 'HarmonyOS Sans SC', 400, f'{H}/HarmonyOS_Sans_SC_Regular.ttf', 0, 'cjk latin'),
    ('songti900', 'Songti SC', 900, f'{S}/Supplemental/Songti.ttc', 0, 'cjk'),
    ('songti700', 'Songti SC', 700, f'{S}/Supplemental/Songti.ttc', 1, 'cjk'),
    ('xingshu', 'hongleixingshu', 400, f'{H}/HongLeiXingShuJianTi-2.otf', 0, 'cjk'),
    # 庞门正道粗书体: heavy brush lettering, strokes rising to the right (free for commercial use).
    ('cushu', 'PangMenZhengDao-Cu', 400, f'{H}/PangMenZhengDaoCuShuTi.ttf', 0, 'cjk'),
    ('impact', 'Impact', 400, f'{S}/Supplemental/Impact.ttf', 0, 'latin'),
    ('arialblack', 'Arial Black', 900, f'{S}/Supplemental/Arial Black.ttf', 0, 'latin'),
    ('futura', 'Futura', 700, f'{S}/Supplemental/Futura.ttc', 2, 'latin'),
    ('rockwell', 'Rockwell', 700, f'{S}/Supplemental/Rockwell.ttc', 2, 'latin'),
    ('typewriter', 'American Typewriter', 700, f'{S}/Supplemental/AmericanTypewriter.ttc', 2, 'latin'),
    ('courier', 'Courier New', 700, f'{S}/Supplemental/Courier New Bold.ttf', 0, 'latin'),
    ('georgia', 'Georgia', 700, f'{S}/Supplemental/Georgia Bold.ttf', 0, 'latin'),
]

# Faces already slanted by design: Chrome's synthetic italic is not tried on them.
SLANTED_FACES = {'youshe', 'smiley', 'cushu'}
