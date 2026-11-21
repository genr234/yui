using UndertaleModLib.Compiler;

EnsureDataLoaded();

var chapter = Environment.GetEnvironmentVariable("YUI_BOSS_CHAPTER");
if (chapter != "2" && chapter != "4")
    throw new InvalidOperationException("Set YUI_BOSS_CHAPTER to 2 or 4.");

// Use the stock scene and encounter ID so the battle retains its native scripts.
(string Name, int Encounter, string Room, string Scene, string Music, string BeforeRoom,
    string BeforeBattle, int Mode, string Marker, string AfterBattleStart, string SpecialEnd, string Result)[] fights =
    chapter == "2" ? new[]
{
    (Name: "queen", Encounter: 59, Room: "room_dw_mansion_east_4f_d", Scene: "obj_ch2_scene25", Music: "queen_boss",
     BeforeRoom: "", BeforeBattle: @"berdly_wire = instance_create(camerax() + 480, cameray() + 140, obj_berdly_wire);
        berdly_wire.visible = false;
        var berdly = instance_create(berdly_wire.x, berdly_wire.y, obj_berdlyplug_enemy);",
     Mode: 1, Marker: "berdly", AfterBattleStart: "",
     SpecialEnd: " || (instance_exists(obj_queen_enemy) && obj_queen_enemy.endcon == 4 && obj_queen_enemy.endtimer >= 1)", Result: "1"),
    ("spamton", 60, "room_dw_city_spamton_alley", "obj_ch2_city05", "spamton_battle",
     "", "global.char[1] = 0; global.char[2] = 0;", 1, "0", "", "", "1"),
    ("spamton-neo", 61, "room_dw_mansion_b_east", "obj_ch2_sceneex2", "spamton_neo_mix_ex_wip",
     "global.flag[309] = 8;", "scr_getchar(2); scr_getchar(3); cityscape_active = true;",
     1, "0", "instance_create(x, y, o_coaster_controller_sneo);", "", "1"),
    ("giga-queen", 84, "room_dw_mansion_top", "obj_ch2_scene26", "boxing_boss",
     "", "global.char[1] = 0; global.char[2] = 0; camerax_set(0); cameray_set(0);",
     2, "0", "", " || (instance_exists(obj_battlecontroller) && obj_battlecontroller.gigaqueencon == 6)", "1"),
}
: new[]
{
    (Name: "gerson", Encounter: 160, Room: "room_dw_church_arena", Scene: "obj_dw_church_arena", Music: "ch4_extra_boss",
     BeforeRoom: "", BeforeBattle: @"global.char[0] = 2;
        global.char[1] = 0;
        global.char[2] = 0;
        camerax_set(520);
        obj_mainchara.x = 620;
        background = instance_create(520, 0, obj_dw_church_arena_bg);
        background.image_alpha = 1;",
     Mode: 1, Marker: "0", AfterBattleStart: "",
     SpecialEnd: " || (instance_exists(obj_hammer_of_justice_enemy) && obj_hammer_of_justice_enemy.endcon == 1 && obj_hammer_of_justice_enemy.end_cutscene_version == 0)",
     Result: "(instance_exists(obj_hammer_of_justice_enemy) && obj_hammer_of_justice_enemy.end_cutscene_version == 0) ? 2 : 1"),
    ("jackenstein", 174, "room_dw_church_jackenstein", "obj_ch4_DCA08D", "pumpkin_boss", "",
     @"camerax_set(1840);
        cameray_set(1740);
        obj_mainchara.x = 1900;
        obj_mainchara.y = 1950;
        obj_mainchara.visible = false;
        jackenstein = instance_create(2122, 1960, obj_npc_jackenstein);
        jackenstein.visible = false;
        jackenstein.darkened = true;
        target = scr_marker_ext(jackenstein.x, jackenstein.y, spr_jackenstein_battle_eyes, 2, 2, undefined, undefined, c_white, 5000);
        target.image_index = 1;",
     1, "0", "", "", "1"),
    ("titan", 175, "room_dw_churchc_titanclimb2_post", "obj_dw_churchc_titanclimb2_post", "titan_battle", "",
     @"with (blackall) visible = false;
        with (titan_actor) visible = false;
        obj_mainchara.x = 3942;
        obj_mainchara.y = 140;
        camerax_set(3800);
        instance_create(camerax(), cameray(), obj_titan_background_balls);
        background_spotlight_alpha = 0.35;
        background_orb_alpha = 1;",
     1, "0", "", " || (instance_exists(obj_titan_enemy) && obj_titan_enemy.acting == 1.25)", "1"),
    ("sound-of-justice", 176, "room_dw_churchb_nongerson", "obj_dw_churchb_nongerson", "statue_chord_basic", "",
     @"with (blackall) visible = false;
        global.char[0] = global.yui_boss == 176 ? 2 : 1;
        global.char[1] = 0;
        global.char[2] = 0;
        global.tempflag[30] = 1;
        global.tempflag[99] = 1;
        if (global.yui_boss == 186)
        {
            savetension = global.yui_sound_tension;
            attackpattern_save = global.yui_sound_pattern;
        }
        target = scr_marker_ext(room_width, room_height, spr_gerson_laugh, 2, 2, undefined, undefined, c_black, 5000);",
     1, "0", "", "", "1"),
};

var changes = new CodeImportGroup(Data);
var launch = "var fight = \"\";\nfor (var i = 1; i <= parameter_count(); i++)\n{\n";
foreach (var boss in fights)
    launch += $"    if (parameter_string(i) == \"yui-{boss.Name}\") fight = \"{boss.Name}\";\n";
launch += "}\n";
foreach (var boss in fights)
{
    var nextPhase = boss.Encounter == 176 ? @"
        if (global.yui_boss == 176)
        {
            global.yui_sound_tension = savetension;
            global.yui_sound_pattern = attackpattern_save;
            global.yui_boss = 186;
            global.yui_state = 0;
            room_restart();
            exit;
        }" : "";

    launch += $@"
if (fight == ""{boss.Name}"")
{{
    scr_gamestart();
    global.yui_boss = {boss.Encounter};
    global.yui_state = 0;
    global.darkzone = 1;
    scr_getchar(2);
    scr_getchar(3);
    {boss.BeforeRoom}
    room_goto({boss.Room});
    exit;
}}
";
    changes.QueuePrepend($"gml_Object_{boss.Scene}_Step_0", $@"
if (variable_global_exists(""yui_boss""))
{{
    if (global.yui_state == 0)
    {{
        snd_free_all();
        global.interact = 1;
        obj_mainchara.cutscene = 1;
        global.flag[9] = 2;
        global.batmusic[0] = snd_init(""{boss.Music}.ogg"");
        var target = 0;
        {boss.BeforeBattle}
        global.flag[54] = global.yui_boss;
        scr_battle(global.yui_boss, {boss.Mode}, target, {boss.Marker}, 0);
        global.flag[9] = 1;
        global.yui_state = 1;
    }}
    else if (global.yui_state == 1 && instance_exists(obj_battlecontroller))
    {{
        {boss.AfterBattleStart}
        global.yui_state = 2;
    }}
    else if (global.yui_state == 2 && (!instance_exists(obj_battlecontroller) || obj_battlecontroller.intro == 2{boss.SpecialEnd}))
    {{
        {nextPhase}
        global.yui_knight_result = {boss.Result};
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
    }}
    exit;
}}
");
}
changes.QueuePrepend($"gml_Object_obj_intro_ch{chapter}_Step_0", launch);
changes.QueuePrepend("gml_Object_DEVICE_MENU_Step_0", launch);
changes.QueuePrepend("gml_Object_PROCESS_LOGO_Draw_0", launch);

const string gameOver = "gml_GlobalScript_scr_gameover";
var source = GetDecompiledText(gameOver);
const string signature = "function scr_gameover()\n{";
var body = source.IndexOf(signature, StringComparison.Ordinal);
if (body < 0)
    throw new InvalidOperationException("The original game-over function was not found.");
changes.QueueReplace(gameOver, source.Insert(body + signature.Length, @"
    if (variable_global_exists(""yui_boss""))
    {
        global.yui_knight_result = 2;
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
        return;
    }
"));
changes.QueuePrepend("gml_Object_obj_gameover_init_Create_0", @"
if (variable_global_exists(""yui_knight_result""))
{
    visible = true;
    depth = -100000;
    exit;
}
");
changes.QueuePrepend("gml_Object_obj_gameover_init_Step_0", @"
if (variable_global_exists(""yui_knight_result"")) exit;
");
changes.QueueReplace("gml_Object_obj_gameover_init_Draw_0", @"
if (variable_global_exists(""yui_knight_result""))
{
    draw_clear(c_black);
    draw_set_alpha(1);
    draw_set_color(c_white);
    draw_set_font(-1);
    draw_set_halign(fa_center);
    draw_set_valign(fa_middle);
    draw_text_transformed(room_width / 2, room_height / 2 - 20,
        global.yui_knight_result == 1 ? ""Victory"" : ""Defeated"", 2, 2, 0);
    draw_text(room_width / 2, room_height / 2 + 20, ""Choose a fight in Yui to play again."");
    draw_set_halign(fa_left);
    draw_set_valign(fa_top);
}
");
changes.Import();
