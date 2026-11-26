#include <metal_stdlib>
#include <simd/simd.h>

using namespace metal;

struct type_GowakuFrame
{
    float2 gowakuViewport;
    float gowakuScale;
    float gowakuPadding;
};

constant float4 _185 = {};

struct main0_out
{
    float4 out_var_SV_Target [[color(0)]];
};

struct main0_in
{
    float2 in_var_LOGICAL [[user(locn0)]];
    float4 in_var_RECT [[user(locn1), flat]];
    float4 in_var_RADII [[user(locn2), flat]];
    float4 in_var_COLOR0 [[user(locn3), flat]];
    float4 in_var_COLOR1 [[user(locn4), flat]];
    float4 in_var_COLOR2 [[user(locn5), flat]];
    float2 in_var_PARAMS [[user(locn7), flat]];
    uint2 in_var_MODE [[user(locn8)]];
    float4 in_var_STOPS [[user(locn11), flat]];
    float4 in_var_TRANSFORM0 [[user(locn12), flat]];
    float2 in_var_TRANSFORM1 [[user(locn13), flat]];
};

fragment main0_out main0(main0_in in [[stage_in]], constant type_GowakuFrame& GowakuFrame [[buffer(0)]], texture2d<float> gowakuTex0 [[texture(1)]], texture2d<float> gowakuBackdrop [[texture(3)]], float4 gl_FragCoord [[position]])
{
    main0_out out = {};
    float4 _2384;
    do
    {
        float _312;
        float2 _313;
        if (any(in.in_var_TRANSFORM0 != float4(0.0)))
        {
            float2 _231 = (gl_FragCoord.xy / float2(GowakuFrame.gowakuScale)) - in.in_var_TRANSFORM1;
            float _233 = _231.x;
            float _236 = _231.y;
            float _247 = (in.in_var_TRANSFORM0.x * in.in_var_TRANSFORM0.w) - (in.in_var_TRANSFORM0.y * in.in_var_TRANSFORM0.z);
            float2 _254 = in.in_var_RECT.zw * 0.5;
            float2 _256 = (in.in_var_LOGICAL - in.in_var_RECT.xy) - _254;
            float2 _258 = abs(_256) - _254;
            float _259 = _258.x;
            float _260 = _258.y;
            bool _273;
            if (_259 > 0.0)
            {
                _273 = _260 > 0.0;
            }
            else
            {
                _273 = false;
            }
            float2 _277;
            if (_273)
            {
                _277 = fast::normalize(_258);
            }
            else
            {
                _277 = select(float2(0.0, 1.0), float2(1.0, 0.0), bool2(_259 > _260));
            }
            float _286;
            if (_256.x < 0.0)
            {
                _286 = -_277.x;
            }
            else
            {
                _286 = _277.x;
            }
            float _295;
            if (_256.y < 0.0)
            {
                _295 = -_277.y;
            }
            else
            {
                _295 = _277.y;
            }
            float2 _296 = float2(_286, _295);
            _312 = fast::clamp(0.5 - ((precise::min(precise::max(_259, _260), 0.0) + length(precise::max(_258, float2(0.0)))) / precise::max(length(float2(dot(_296, float2(in.in_var_TRANSFORM0.w, -in.in_var_TRANSFORM0.y)), dot(_296, float2(-in.in_var_TRANSFORM0.z, in.in_var_TRANSFORM0.x)))) / (abs(_247) * GowakuFrame.gowakuScale), 9.9999999600419720025001879548654e-13)), 0.0, 1.0);
            _313 = fast::clamp((float2((in.in_var_TRANSFORM0.w * _233) - (in.in_var_TRANSFORM0.z * _236), (in.in_var_TRANSFORM0.x * _236) - (in.in_var_TRANSFORM0.y * _233)) / float2(_247)) - in.in_var_RECT.xy, float2(0.0), in.in_var_RECT.zw);
        }
        else
        {
            _312 = 1.0;
            _313 = (gl_FragCoord.xy / float2(GowakuFrame.gowakuScale)) - in.in_var_RECT.xy;
        }
        float2 _315 = _313 / float2((in.in_var_PARAMS.y > 0.0) ? in.in_var_PARAMS.y : 1.0);
        float4 _1677;
        do
        {
            bool _330;
            if ((isunordered(in.in_var_RADII.z, 0.0) || in.in_var_RADII.z > 0.0))
            {
                _330 = in.in_var_RADII.w <= 0.0;
            }
            else
            {
                _330 = true;
            }
            if (_330)
            {
                _1677 = float4(0.054999999701976776123046875, 0.0670000016689300537109375, 0.08200000226497650146484375, 1.0);
                break;
            }
            float _335 = (_315.x - in.in_var_RADII.x) / in.in_var_RADII.z;
            float _338 = (_315.y - in.in_var_RADII.y) / in.in_var_RADII.w;
            float _340 = 0.5 * (_335 + _338);
            float _344 = (0.5 * (_338 - _335)) - 24.5;
            float _347 = ((_340 - 24.5) * in.in_var_STOPS.y) + (_344 * in.in_var_STOPS.z);
            float _348 = _347 + 24.5;
            float _352 = ((24.5 - _340) * in.in_var_STOPS.z) + (_344 * in.in_var_STOPS.y);
            float _353 = _352 + 24.5;
            float2 _354 = float2(_348, _353);
            float2 _359 = float2(0.5 * (in.in_var_STOPS.y + in.in_var_STOPS.z), 0.5 * (in.in_var_STOPS.y - in.in_var_STOPS.z));
            float _361 = (17.200000762939453125 * in.in_var_STOPS.x) / in.in_var_RADII.w;
            float2 _363 = _354 + (_359 * _361);
            float2 _364 = -_359;
            float2 _366 = floor(_363 + float2(0.5));
            float _367 = _364.x;
            bool _368 = _367 >= 0.0;
            float _369 = _368 ? 1.0 : (-1.0);
            float _370 = _364.y;
            bool _371 = _370 >= 0.0;
            float _372 = _371 ? 1.0 : (-1.0);
            float _374 = 0.5;
            float _375 = -0.5;
            float _379 = 0.5;
            float _380 = -0.5;
            bool _384 = abs(_367) > 9.9999997473787516355514526367188e-06;
            float _391;
            if (_384)
            {
                _391 = ((_366.x + (_368 ? 0.5 : (-0.5))) - _363.x) / _367;
            }
            else
            {
                _391 = 1000000000.0;
            }
            bool _393 = abs(_370) > 9.9999997473787516355514526367188e-06;
            float _400;
            if (_393)
            {
                _400 = ((_366.y + (_371 ? 0.5 : (-0.5))) - _363.y) / _370;
            }
            else
            {
                _400 = 1000000000.0;
            }
            float _406;
            if (_384)
            {
                _406 = abs(1.0 / _367);
            }
            else
            {
                _406 = 1000000000.0;
            }
            float _412;
            if (_393)
            {
                _412 = abs(1.0 / _370);
            }
            else
            {
                _412 = 1000000000.0;
            }
            float2 _413 = fast::normalize(float2(-0.75, -0.550000011920928955078125));
            float2 _415;
            float2 _426;
            _415 = _366;
            _426 = float2(0.0);
            float _419;
            float2 _416;
            float _421;
            float _423;
            float2 _427;
            float4 _703;
            bool _704;
            float _418 = 0.0;
            float _420 = _400;
            float _422 = _391;
            int _424 = 0;
            for (;;)
            {
                if (_424 < 8)
                {
                    float _432 = precise::min(precise::min(_422, _420), _361);
                    float _433 = _361 - _418;
                    float4 _563;
                    do
                    {
                        float _439 = _415.x + 100.0;
                        float _440 = _415.y + 100.0;
                        bool _445;
                        if ((isunordered(_439, 0.0) || _439 >= 0.0))
                        {
                            _445 = _439 >= 250.0;
                        }
                        else
                        {
                            _445 = true;
                        }
                        bool _450;
                        if (!_445)
                        {
                            _450 = _440 < 0.0;
                        }
                        else
                        {
                            _450 = true;
                        }
                        bool _455;
                        if (!_450)
                        {
                            _455 = _440 >= 250.0;
                        }
                        else
                        {
                            _455 = true;
                        }
                        if (_455)
                        {
                            _563 = float4(0.0);
                            break;
                        }
                        float2 _471;
                        float2 _464 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_439, _440) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                        float2 _465 = floor(_464);
                        float2 _466 = _464 - _465;
                        int2 _467 = int2(_465);
                        float4 _489;
                        do
                        {
                            float2 _470 = float2(_467);
                            _471 = in.in_var_COLOR2.xy;
                            bool _480;
                            if (!any(_470 < _471))
                            {
                                _480 = any(_470 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _480 = true;
                            }
                            if (_480)
                            {
                                _489 = float4(0.0);
                                break;
                            }
                            _489 = gowakuTex0.read(uint2(int3(_467, 0).xy), 0);
                            break;
                        } while(false);
                        int2 _490 = _467 + int2(1, 0);
                        float4 _511;
                        do
                        {
                            float2 _493 = float2(_490);
                            bool _502;
                            if (!any(_493 < _471))
                            {
                                _502 = any(_493 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _502 = true;
                            }
                            if (_502)
                            {
                                _511 = float4(0.0);
                                break;
                            }
                            _511 = gowakuTex0.read(uint2(int3(_490, 0).xy), 0);
                            break;
                        } while(false);
                        float4 _513 = float4(_466.x);
                        int2 _515 = _467 + int2(0, 1);
                        float4 _536;
                        do
                        {
                            float2 _518 = float2(_515);
                            bool _527;
                            if (!any(_518 < _471))
                            {
                                _527 = any(_518 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _527 = true;
                            }
                            if (_527)
                            {
                                _536 = float4(0.0);
                                break;
                            }
                            _536 = gowakuTex0.read(uint2(int3(_515, 0).xy), 0);
                            break;
                        } while(false);
                        int2 _537 = _467 + int2(1);
                        float4 _558;
                        do
                        {
                            float2 _540 = float2(_537);
                            bool _549;
                            if (!any(_540 < _471))
                            {
                                _549 = any(_540 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _549 = true;
                            }
                            if (_549)
                            {
                                _558 = float4(0.0);
                                break;
                            }
                            _558 = gowakuTex0.read(uint2(int3(_537, 0).xy), 0);
                            break;
                        } while(false);
                        _563 = mix(mix(_489, _511, _513), mix(_536, _558, _513), float4(_466.y));
                        break;
                    } while(false);
                    bool _570;
                    if (_563.z >= 0.25)
                    {
                        _570 = _563.x >= 0.62000000476837158203125;
                    }
                    else
                    {
                        _570 = false;
                    }
                    if (_570)
                    {
                        float _577 = precise::max(1.0, floor((_563.y * 3.0) + 0.5));
                        float2 _584 = fract(float2(fmod(_415.x + 1000.0, 50.0), fmod(_415.y + 1000.0, 50.0)) * float2(123.339996337890625, 456.209991455078125));
                        float2 _588 = _584 + float2(dot(_584, _584 + float2(45.31999969482421875)));
                        float _599 = (((4.0 + (4.0 * _577)) + ((fract(_588.x * _588.y) - 0.5) * 0.89999997615814208984375)) * in.in_var_STOPS.x) / in.in_var_RADII.w;
                        if (_599 >= (_361 - _432))
                        {
                            float _604 = (_563.z > 0.75) ? 1.0 : 0.7799999713897705078125;
                            float2 _608 = fract(((_415 * 1.7000000476837158203125) + float2(3.099999904632568359375)) * float2(123.339996337890625, 456.209991455078125));
                            float2 _612 = _608 + float2(dot(_608, _608 + float2(45.31999969482421875)));
                            float _617 = fract(_612.x * _612.y) - 0.5;
                            if (_599 <= _433)
                            {
                                float2 _645 = (_354 + (_359 * _599)) - _415;
                                float3 _653;
                                if (_577 >= 3.0)
                                {
                                    _653 = float3(0.324999988079071044921875, 0.361999988555908203125, 0.435000002384185791015625);
                                }
                                else
                                {
                                    _653 = select(float3(0.23499999940395355224609375, 0.2619999945163726806640625, 0.3179999887943267822265625), float3(0.27799999713897705078125, 0.310000002384185791015625, 0.375999987125396728515625), bool3(_577 >= 2.0));
                                }
                                float _654 = dot(_645, _413);
                                float3 _659 = _653 + float3(fast::clamp(0.070000000298023223876953125 * _654, -0.039999999105930328369140625, 0.0599999986588954925537109375) + (_617 * 0.02500000037252902984619140625));
                                float3 _679;
                                if (in.in_var_RADII.z >= 5.0)
                                {
                                    float _669 = smoothstep(0.7599999904632568359375, 0.959999978542327880859375, precise::max(abs(_645.x), abs(_645.y)) * 2.0);
                                    float _676;
                                    if (_654 > 0.0)
                                    {
                                        _676 = 0.04500000178813934326171875 * _669;
                                    }
                                    else
                                    {
                                        _676 = (-0.054999999701976776123046875) * _669;
                                    }
                                    _679 = _659 + float3(_676);
                                }
                                else
                                {
                                    _679 = _659;
                                }
                                _703 = float4(fast::clamp(_679 * _604, float3(0.0), float3(1.0)), 1.0);
                                _704 = true;
                                break;
                            }
                            else
                            {
                                _703 = float4(fast::clamp(((mix(float3(0.122000001370906829833984375, 0.14000000059604644775390625, 0.17499999701976776123046875), float3(0.185000002384185791015625, 0.208000004291534423828125, 0.25499999523162841796875), float3((dot(_426, _413) * 0.5) + 0.5)) + float3(_617 * 0.013750000856816768646240234375)) * mix(0.730000019073486328125, 1.03999996185302734375, smoothstep(0.0, 0.75, fast::clamp(_433 / precise::max(_599, 0.00999999977648258209228515625), 0.0, 1.0)))) * _604, float3(0.0), float3(1.0)), 1.0);
                                _704 = true;
                                break;
                            }
                        }
                    }
                    if (_432 >= _361)
                    {
                        _703 = _185;
                        _704 = false;
                        break;
                    }
                    bool _689 = _422 < _420;
                    if (_689)
                    {
                        float2 _699 = _415;
                        _699.x = _415.x + _369;
                        _416 = _699;
                        _421 = _420;
                        _423 = _422 + _406;
                        _427 = float2(-_369, 0.0);
                    }
                    else
                    {
                        float2 _694 = _415;
                        _694.y = _415.y + _372;
                        _416 = _694;
                        _421 = _420 + _412;
                        _423 = _422;
                        _427 = float2(0.0, -_372);
                    }
                    _419 = _689 ? _422 : _420;
                    _415 = _416;
                    _418 = _419;
                    _420 = _421;
                    _422 = _423;
                    _424++;
                    _426 = _427;
                    continue;
                }
                else
                {
                    _703 = _185;
                    _704 = false;
                    break;
                }
            }
            if (_704)
            {
                _1677 = _703;
                break;
            }
            float _712;
            float _713;
            bool _714;
            float _707 = floor(_347 + 25.0);
            float _709 = floor(_352 + 25.0);
            float4 _836;
            do
            {
                _712 = _707 + 100.0;
                _713 = _709 + 100.0;
                _714 = (isunordered(_712, 0.0) || _712 >= 0.0);
                bool _718;
                if (_714)
                {
                    _718 = _712 >= 250.0;
                }
                else
                {
                    _718 = true;
                }
                bool _723;
                if (!_718)
                {
                    _723 = _713 < 0.0;
                }
                else
                {
                    _723 = true;
                }
                bool _728;
                if (!_723)
                {
                    _728 = _713 >= 250.0;
                }
                else
                {
                    _728 = true;
                }
                if (_728)
                {
                    _836 = float4(0.0);
                    break;
                }
                float2 _744;
                float2 _737 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_712, _713) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                float2 _738 = floor(_737);
                float2 _739 = _737 - _738;
                int2 _740 = int2(_738);
                float4 _762;
                do
                {
                    float2 _743 = float2(_740);
                    _744 = in.in_var_COLOR2.xy;
                    bool _753;
                    if (!any(_743 < _744))
                    {
                        _753 = any(_743 >= in.in_var_COLOR2.zw);
                    }
                    else
                    {
                        _753 = true;
                    }
                    if (_753)
                    {
                        _762 = float4(0.0);
                        break;
                    }
                    _762 = gowakuTex0.read(uint2(int3(_740, 0).xy), 0);
                    break;
                } while(false);
                int2 _763 = _740 + int2(1, 0);
                float4 _784;
                do
                {
                    float2 _766 = float2(_763);
                    bool _775;
                    if (!any(_766 < _744))
                    {
                        _775 = any(_766 >= in.in_var_COLOR2.zw);
                    }
                    else
                    {
                        _775 = true;
                    }
                    if (_775)
                    {
                        _784 = float4(0.0);
                        break;
                    }
                    _784 = gowakuTex0.read(uint2(int3(_763, 0).xy), 0);
                    break;
                } while(false);
                float4 _786 = float4(_739.x);
                int2 _788 = _740 + int2(0, 1);
                float4 _809;
                do
                {
                    float2 _791 = float2(_788);
                    bool _800;
                    if (!any(_791 < _744))
                    {
                        _800 = any(_791 >= in.in_var_COLOR2.zw);
                    }
                    else
                    {
                        _800 = true;
                    }
                    if (_800)
                    {
                        _809 = float4(0.0);
                        break;
                    }
                    _809 = gowakuTex0.read(uint2(int3(_788, 0).xy), 0);
                    break;
                } while(false);
                int2 _810 = _740 + int2(1);
                float4 _831;
                do
                {
                    float2 _813 = float2(_810);
                    bool _822;
                    if (!any(_813 < _744))
                    {
                        _822 = any(_813 >= in.in_var_COLOR2.zw);
                    }
                    else
                    {
                        _822 = true;
                    }
                    if (_822)
                    {
                        _831 = float4(0.0);
                        break;
                    }
                    _831 = gowakuTex0.read(uint2(int3(_810, 0).xy), 0);
                    break;
                } while(false);
                _836 = mix(mix(_762, _784, _786), mix(_809, _831, _786), float4(_739.y));
                break;
            } while(false);
            if (_836.z < 0.25)
            {
                _1677 = float4(0.054999999701976776123046875, 0.0670000016689300537109375, 0.08200000226497650146484375, 1.0);
                break;
            }
            float _841 = _348 - _707;
            float _842 = _353 - _709;
            bool _848;
            if (_836.x > 0.37000000476837158203125)
            {
                _848 = _836.x < 0.62000000476837158203125;
            }
            else
            {
                _848 = false;
            }
            float3 _877;
            if (_848)
            {
                _877 = mix(float3(0.086000002920627593994140625, 0.16500000655651092529296875, 0.122000001370906829833984375), float3(0.12800000607967376708984375, 0.23199999332427978515625, 0.1739999949932098388671875), float3((((sin(((_348 * 1.64999997615814208984375) + (_353 * 1.14999997615814208984375)) + (in.in_var_PARAMS.x * 0.699999988079071044921875)) * cos(((_348 * 0.949999988079071044921875) - (_353 * 1.85000002384185791015625)) - (in.in_var_PARAMS.x * 0.550000011920928955078125))) * 0.5) + 0.5) * 0.4199999868869781494140625));
            }
            else
            {
                _877 = select(float3(0.100000001490116119384765625, 0.115999996662139892578125, 0.1459999978542327880859375), float3(0.11200000345706939697265625, 0.12899999320507049560546875, 0.16099999845027923583984375), bool3(abs(fmod((_707 + _709) + 1000.0, 2.0)) < 0.5));
            }
            float _882;
            bool _883;
            float _1020;
            do
            {
                float4 _1005;
                do
                {
                    _882 = _707 + 99.0;
                    _883 = (isunordered(_882, 0.0) || _882 >= 0.0);
                    bool _887;
                    if (_883)
                    {
                        _887 = _882 >= 250.0;
                    }
                    else
                    {
                        _887 = true;
                    }
                    bool _892;
                    if (!_887)
                    {
                        _892 = _713 < 0.0;
                    }
                    else
                    {
                        _892 = true;
                    }
                    bool _897;
                    if (!_892)
                    {
                        _897 = _713 >= 250.0;
                    }
                    else
                    {
                        _897 = true;
                    }
                    if (_897)
                    {
                        _1005 = float4(0.0);
                        break;
                    }
                    float2 _913;
                    float2 _906 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_882, _713) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                    float2 _907 = floor(_906);
                    float2 _908 = _906 - _907;
                    int2 _909 = int2(_907);
                    float4 _931;
                    do
                    {
                        float2 _912 = float2(_909);
                        _913 = in.in_var_COLOR2.xy;
                        bool _922;
                        if (!any(_912 < _913))
                        {
                            _922 = any(_912 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _922 = true;
                        }
                        if (_922)
                        {
                            _931 = float4(0.0);
                            break;
                        }
                        _931 = gowakuTex0.read(uint2(int3(_909, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _932 = _909 + int2(1, 0);
                    float4 _953;
                    do
                    {
                        float2 _935 = float2(_932);
                        bool _944;
                        if (!any(_935 < _913))
                        {
                            _944 = any(_935 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _944 = true;
                        }
                        if (_944)
                        {
                            _953 = float4(0.0);
                            break;
                        }
                        _953 = gowakuTex0.read(uint2(int3(_932, 0).xy), 0);
                        break;
                    } while(false);
                    float4 _955 = float4(_908.x);
                    int2 _957 = _909 + int2(0, 1);
                    float4 _978;
                    do
                    {
                        float2 _960 = float2(_957);
                        bool _969;
                        if (!any(_960 < _913))
                        {
                            _969 = any(_960 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _969 = true;
                        }
                        if (_969)
                        {
                            _978 = float4(0.0);
                            break;
                        }
                        _978 = gowakuTex0.read(uint2(int3(_957, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _979 = _909 + int2(1);
                    float4 _1000;
                    do
                    {
                        float2 _982 = float2(_979);
                        bool _991;
                        if (!any(_982 < _913))
                        {
                            _991 = any(_982 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _991 = true;
                        }
                        if (_991)
                        {
                            _1000 = float4(0.0);
                            break;
                        }
                        _1000 = gowakuTex0.read(uint2(int3(_979, 0).xy), 0);
                        break;
                    } while(false);
                    _1005 = mix(mix(_931, _953, _955), mix(_978, _1000, _955), float4(_908.y));
                    break;
                } while(false);
                bool _1012;
                if ((isunordered(_1005.z, 0.25) || _1005.z >= 0.25))
                {
                    _1012 = _1005.x < 0.62000000476837158203125;
                }
                else
                {
                    _1012 = true;
                }
                if (_1012)
                {
                    _1020 = 0.0;
                    break;
                }
                _1020 = precise::max(1.0, floor((_1005.y * 3.0) + 0.5));
                break;
            } while(false);
            float _1025;
            float _1162;
            do
            {
                float4 _1147;
                do
                {
                    _1025 = _709 + 99.0;
                    bool _1029;
                    if (_714)
                    {
                        _1029 = _712 >= 250.0;
                    }
                    else
                    {
                        _1029 = true;
                    }
                    bool _1034;
                    if (!_1029)
                    {
                        _1034 = _1025 < 0.0;
                    }
                    else
                    {
                        _1034 = true;
                    }
                    bool _1039;
                    if (!_1034)
                    {
                        _1039 = _1025 >= 250.0;
                    }
                    else
                    {
                        _1039 = true;
                    }
                    if (_1039)
                    {
                        _1147 = float4(0.0);
                        break;
                    }
                    float2 _1055;
                    float2 _1048 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_712, _1025) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                    float2 _1049 = floor(_1048);
                    float2 _1050 = _1048 - _1049;
                    int2 _1051 = int2(_1049);
                    float4 _1073;
                    do
                    {
                        float2 _1054 = float2(_1051);
                        _1055 = in.in_var_COLOR2.xy;
                        bool _1064;
                        if (!any(_1054 < _1055))
                        {
                            _1064 = any(_1054 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1064 = true;
                        }
                        if (_1064)
                        {
                            _1073 = float4(0.0);
                            break;
                        }
                        _1073 = gowakuTex0.read(uint2(int3(_1051, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1074 = _1051 + int2(1, 0);
                    float4 _1095;
                    do
                    {
                        float2 _1077 = float2(_1074);
                        bool _1086;
                        if (!any(_1077 < _1055))
                        {
                            _1086 = any(_1077 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1086 = true;
                        }
                        if (_1086)
                        {
                            _1095 = float4(0.0);
                            break;
                        }
                        _1095 = gowakuTex0.read(uint2(int3(_1074, 0).xy), 0);
                        break;
                    } while(false);
                    float4 _1097 = float4(_1050.x);
                    int2 _1099 = _1051 + int2(0, 1);
                    float4 _1120;
                    do
                    {
                        float2 _1102 = float2(_1099);
                        bool _1111;
                        if (!any(_1102 < _1055))
                        {
                            _1111 = any(_1102 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1111 = true;
                        }
                        if (_1111)
                        {
                            _1120 = float4(0.0);
                            break;
                        }
                        _1120 = gowakuTex0.read(uint2(int3(_1099, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1121 = _1051 + int2(1);
                    float4 _1142;
                    do
                    {
                        float2 _1124 = float2(_1121);
                        bool _1133;
                        if (!any(_1124 < _1055))
                        {
                            _1133 = any(_1124 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1133 = true;
                        }
                        if (_1133)
                        {
                            _1142 = float4(0.0);
                            break;
                        }
                        _1142 = gowakuTex0.read(uint2(int3(_1121, 0).xy), 0);
                        break;
                    } while(false);
                    _1147 = mix(mix(_1073, _1095, _1097), mix(_1120, _1142, _1097), float4(_1050.y));
                    break;
                } while(false);
                bool _1154;
                if ((isunordered(_1147.z, 0.25) || _1147.z >= 0.25))
                {
                    _1154 = _1147.x < 0.62000000476837158203125;
                }
                else
                {
                    _1154 = true;
                }
                if (_1154)
                {
                    _1162 = 0.0;
                    break;
                }
                _1162 = precise::max(1.0, floor((_1147.y * 3.0) + 0.5));
                break;
            } while(false);
            float _1305;
            do
            {
                float4 _1290;
                do
                {
                    float _1167 = _707 + 101.0;
                    bool _1172;
                    if ((isunordered(_1167, 0.0) || _1167 >= 0.0))
                    {
                        _1172 = _1167 >= 250.0;
                    }
                    else
                    {
                        _1172 = true;
                    }
                    bool _1177;
                    if (!_1172)
                    {
                        _1177 = _713 < 0.0;
                    }
                    else
                    {
                        _1177 = true;
                    }
                    bool _1182;
                    if (!_1177)
                    {
                        _1182 = _713 >= 250.0;
                    }
                    else
                    {
                        _1182 = true;
                    }
                    if (_1182)
                    {
                        _1290 = float4(0.0);
                        break;
                    }
                    float2 _1198;
                    float2 _1191 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_1167, _713) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                    float2 _1192 = floor(_1191);
                    float2 _1193 = _1191 - _1192;
                    int2 _1194 = int2(_1192);
                    float4 _1216;
                    do
                    {
                        float2 _1197 = float2(_1194);
                        _1198 = in.in_var_COLOR2.xy;
                        bool _1207;
                        if (!any(_1197 < _1198))
                        {
                            _1207 = any(_1197 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1207 = true;
                        }
                        if (_1207)
                        {
                            _1216 = float4(0.0);
                            break;
                        }
                        _1216 = gowakuTex0.read(uint2(int3(_1194, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1217 = _1194 + int2(1, 0);
                    float4 _1238;
                    do
                    {
                        float2 _1220 = float2(_1217);
                        bool _1229;
                        if (!any(_1220 < _1198))
                        {
                            _1229 = any(_1220 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1229 = true;
                        }
                        if (_1229)
                        {
                            _1238 = float4(0.0);
                            break;
                        }
                        _1238 = gowakuTex0.read(uint2(int3(_1217, 0).xy), 0);
                        break;
                    } while(false);
                    float4 _1240 = float4(_1193.x);
                    int2 _1242 = _1194 + int2(0, 1);
                    float4 _1263;
                    do
                    {
                        float2 _1245 = float2(_1242);
                        bool _1254;
                        if (!any(_1245 < _1198))
                        {
                            _1254 = any(_1245 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1254 = true;
                        }
                        if (_1254)
                        {
                            _1263 = float4(0.0);
                            break;
                        }
                        _1263 = gowakuTex0.read(uint2(int3(_1242, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1264 = _1194 + int2(1);
                    float4 _1285;
                    do
                    {
                        float2 _1267 = float2(_1264);
                        bool _1276;
                        if (!any(_1267 < _1198))
                        {
                            _1276 = any(_1267 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1276 = true;
                        }
                        if (_1276)
                        {
                            _1285 = float4(0.0);
                            break;
                        }
                        _1285 = gowakuTex0.read(uint2(int3(_1264, 0).xy), 0);
                        break;
                    } while(false);
                    _1290 = mix(mix(_1216, _1238, _1240), mix(_1263, _1285, _1240), float4(_1193.y));
                    break;
                } while(false);
                bool _1297;
                if ((isunordered(_1290.z, 0.25) || _1290.z >= 0.25))
                {
                    _1297 = _1290.x < 0.62000000476837158203125;
                }
                else
                {
                    _1297 = true;
                }
                if (_1297)
                {
                    _1305 = 0.0;
                    break;
                }
                _1305 = precise::max(1.0, floor((_1290.y * 3.0) + 0.5));
                break;
            } while(false);
            float _1447;
            do
            {
                float4 _1432;
                do
                {
                    float _1310 = _709 + 101.0;
                    bool _1314;
                    if (_714)
                    {
                        _1314 = _712 >= 250.0;
                    }
                    else
                    {
                        _1314 = true;
                    }
                    bool _1319;
                    if (!_1314)
                    {
                        _1319 = _1310 < 0.0;
                    }
                    else
                    {
                        _1319 = true;
                    }
                    bool _1324;
                    if (!_1319)
                    {
                        _1324 = _1310 >= 250.0;
                    }
                    else
                    {
                        _1324 = true;
                    }
                    if (_1324)
                    {
                        _1432 = float4(0.0);
                        break;
                    }
                    float2 _1340;
                    float2 _1333 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_712, _1310) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                    float2 _1334 = floor(_1333);
                    float2 _1335 = _1333 - _1334;
                    int2 _1336 = int2(_1334);
                    float4 _1358;
                    do
                    {
                        float2 _1339 = float2(_1336);
                        _1340 = in.in_var_COLOR2.xy;
                        bool _1349;
                        if (!any(_1339 < _1340))
                        {
                            _1349 = any(_1339 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1349 = true;
                        }
                        if (_1349)
                        {
                            _1358 = float4(0.0);
                            break;
                        }
                        _1358 = gowakuTex0.read(uint2(int3(_1336, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1359 = _1336 + int2(1, 0);
                    float4 _1380;
                    do
                    {
                        float2 _1362 = float2(_1359);
                        bool _1371;
                        if (!any(_1362 < _1340))
                        {
                            _1371 = any(_1362 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1371 = true;
                        }
                        if (_1371)
                        {
                            _1380 = float4(0.0);
                            break;
                        }
                        _1380 = gowakuTex0.read(uint2(int3(_1359, 0).xy), 0);
                        break;
                    } while(false);
                    float4 _1382 = float4(_1335.x);
                    int2 _1384 = _1336 + int2(0, 1);
                    float4 _1405;
                    do
                    {
                        float2 _1387 = float2(_1384);
                        bool _1396;
                        if (!any(_1387 < _1340))
                        {
                            _1396 = any(_1387 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1396 = true;
                        }
                        if (_1396)
                        {
                            _1405 = float4(0.0);
                            break;
                        }
                        _1405 = gowakuTex0.read(uint2(int3(_1384, 0).xy), 0);
                        break;
                    } while(false);
                    int2 _1406 = _1336 + int2(1);
                    float4 _1427;
                    do
                    {
                        float2 _1409 = float2(_1406);
                        bool _1418;
                        if (!any(_1409 < _1340))
                        {
                            _1418 = any(_1409 >= in.in_var_COLOR2.zw);
                        }
                        else
                        {
                            _1418 = true;
                        }
                        if (_1418)
                        {
                            _1427 = float4(0.0);
                            break;
                        }
                        _1427 = gowakuTex0.read(uint2(int3(_1406, 0).xy), 0);
                        break;
                    } while(false);
                    _1432 = mix(mix(_1358, _1380, _1382), mix(_1405, _1427, _1382), float4(_1335.y));
                    break;
                } while(false);
                bool _1439;
                if ((isunordered(_1432.z, 0.25) || _1432.z >= 0.25))
                {
                    _1439 = _1432.x < 0.62000000476837158203125;
                }
                else
                {
                    _1439 = true;
                }
                if (_1439)
                {
                    _1447 = 0.0;
                    break;
                }
                _1447 = precise::max(1.0, floor((_1432.y * 3.0) + 0.5));
                break;
            } while(false);
            float _1461;
            if (_1020 > 0.0)
            {
                _1461 = precise::max(0.0, (1.0 - smoothstep(0.0, fast::clamp(0.449999988079071044921875 + (0.180000007152557373046875 * _1020), 0.449999988079071044921875, 0.949999988079071044921875), _841 + 0.5)) * (0.2800000011920928955078125 + (0.0599999986588954925537109375 * _1020)));
            }
            else
            {
                _1461 = 0.0;
            }
            float _1472;
            if (_1162 > 0.0)
            {
                _1472 = precise::max(_1461, (1.0 - smoothstep(0.0, 0.550000011920928955078125, _842 + 0.5)) * (0.180000007152557373046875 + (0.039999999105930328369140625 * _1162)));
            }
            else
            {
                _1472 = _1461;
            }
            float _1481;
            if (_1305 > 0.0)
            {
                _1481 = precise::max(_1472, (1.0 - smoothstep(0.0, 0.319999992847442626953125, 0.5 - _841)) * 0.20000000298023223876953125);
            }
            else
            {
                _1481 = _1472;
            }
            float _1490;
            if (_1447 > 0.0)
            {
                _1490 = precise::max(_1481, (1.0 - smoothstep(0.0, 0.319999992847442626953125, 0.5 - _842)) * 0.2199999988079071044921875);
            }
            else
            {
                _1490 = _1481;
            }
            bool _1636;
            if (_1490 < 0.25)
            {
                float _1634;
                do
                {
                    float4 _1619;
                    do
                    {
                        bool _1501;
                        if (_883)
                        {
                            _1501 = _882 >= 250.0;
                        }
                        else
                        {
                            _1501 = true;
                        }
                        bool _1506;
                        if (!_1501)
                        {
                            _1506 = _1025 < 0.0;
                        }
                        else
                        {
                            _1506 = true;
                        }
                        bool _1511;
                        if (!_1506)
                        {
                            _1511 = _1025 >= 250.0;
                        }
                        else
                        {
                            _1511 = true;
                        }
                        if (_1511)
                        {
                            _1619 = float4(0.0);
                            break;
                        }
                        float2 _1527;
                        float2 _1520 = mix(in.in_var_COLOR1.xy, in.in_var_COLOR1.zw, (float2(_882, _1025) + float2(0.5)) * float2(0.0040000001899898052215576171875)) - float2(0.5);
                        float2 _1521 = floor(_1520);
                        float2 _1522 = _1520 - _1521;
                        int2 _1523 = int2(_1521);
                        float4 _1545;
                        do
                        {
                            float2 _1526 = float2(_1523);
                            _1527 = in.in_var_COLOR2.xy;
                            bool _1536;
                            if (!any(_1526 < _1527))
                            {
                                _1536 = any(_1526 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _1536 = true;
                            }
                            if (_1536)
                            {
                                _1545 = float4(0.0);
                                break;
                            }
                            _1545 = gowakuTex0.read(uint2(int3(_1523, 0).xy), 0);
                            break;
                        } while(false);
                        int2 _1546 = _1523 + int2(1, 0);
                        float4 _1567;
                        do
                        {
                            float2 _1549 = float2(_1546);
                            bool _1558;
                            if (!any(_1549 < _1527))
                            {
                                _1558 = any(_1549 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _1558 = true;
                            }
                            if (_1558)
                            {
                                _1567 = float4(0.0);
                                break;
                            }
                            _1567 = gowakuTex0.read(uint2(int3(_1546, 0).xy), 0);
                            break;
                        } while(false);
                        float4 _1569 = float4(_1522.x);
                        int2 _1571 = _1523 + int2(0, 1);
                        float4 _1592;
                        do
                        {
                            float2 _1574 = float2(_1571);
                            bool _1583;
                            if (!any(_1574 < _1527))
                            {
                                _1583 = any(_1574 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _1583 = true;
                            }
                            if (_1583)
                            {
                                _1592 = float4(0.0);
                                break;
                            }
                            _1592 = gowakuTex0.read(uint2(int3(_1571, 0).xy), 0);
                            break;
                        } while(false);
                        int2 _1593 = _1523 + int2(1);
                        float4 _1614;
                        do
                        {
                            float2 _1596 = float2(_1593);
                            bool _1605;
                            if (!any(_1596 < _1527))
                            {
                                _1605 = any(_1596 >= in.in_var_COLOR2.zw);
                            }
                            else
                            {
                                _1605 = true;
                            }
                            if (_1605)
                            {
                                _1614 = float4(0.0);
                                break;
                            }
                            _1614 = gowakuTex0.read(uint2(int3(_1593, 0).xy), 0);
                            break;
                        } while(false);
                        _1619 = mix(mix(_1545, _1567, _1569), mix(_1592, _1614, _1569), float4(_1522.y));
                        break;
                    } while(false);
                    bool _1626;
                    if ((isunordered(_1619.z, 0.25) || _1619.z >= 0.25))
                    {
                        _1626 = _1619.x < 0.62000000476837158203125;
                    }
                    else
                    {
                        _1626 = true;
                    }
                    if (_1626)
                    {
                        _1634 = 0.0;
                        break;
                    }
                    _1634 = precise::max(1.0, floor((_1619.y * 3.0) + 0.5));
                    break;
                } while(false);
                _1636 = _1634 > 0.0;
            }
            else
            {
                _1636 = false;
            }
            float _1647;
            if (_1636)
            {
                _1647 = precise::max(_1490, (1.0 - smoothstep(0.0, 0.550000011920928955078125, length(float2(_841 + 0.5, _842 + 0.5)))) * 0.2199999988079071044921875);
            }
            else
            {
                _1647 = _1490;
            }
            float3 _1649 = _877 * (1.0 - _1647);
            bool _1650 = _836.z > 0.75;
            float3 _1655;
            if (!_1650)
            {
                _1655 = _1649 * 0.7599999904632568359375;
            }
            else
            {
                _1655 = _1649;
            }
            float3 _1671;
            if (in.in_var_RADII.z >= 5.0)
            {
                _1671 = mix(_1655, float3(0.0379999987781047821044921875, 0.048000000417232513427734375, 0.064999997615814208984375), float3((1.0 - smoothstep(0.0, 0.89999997615814208984375, precise::min(0.5 - abs(_841), 0.5 - abs(_842)) * in.in_var_RADII.z)) * (_1650 ? 0.319999992847442626953125 : 0.17999999225139617919921875)));
            }
            else
            {
                _1671 = _1655;
            }
            _1677 = float4(fast::clamp(_1671, float3(0.0), float3(1.0)), 1.0);
            break;
        } while(false);
        float4 _1680 = _1677 * (in.in_var_COLOR0.w * _312);
        uint _1682 = in.in_var_MODE.x >> 8u;
        if (_1682 == 0u)
        {
            _2384 = _1680;
            break;
        }
        float4 _2383;
        do
        {
            if (_1682 == 1u)
            {
                _2383 = float4(0.0, 0.0, 0.0, _312);
                break;
            }
            bool _1696;
            if (_1682 != 6u)
            {
                _1696 = _1682 == 14u;
            }
            else
            {
                _1696 = true;
            }
            if (_1696)
            {
                _2383 = float4(_312) - _1680;
                break;
            }
            bool _1701 = _1682 == 2u;
            bool _1706;
            if (!_1701)
            {
                _1706 = _1682 == 5u;
            }
            else
            {
                _1706 = true;
            }
            bool _1711;
            if (!_1706)
            {
                _1711 = _1682 == 7u;
            }
            else
            {
                _1711 = true;
            }
            bool _1716;
            if (!_1711)
            {
                _1716 = _1682 == 10u;
            }
            else
            {
                _1716 = true;
            }
            bool _1721;
            if (!_1716)
            {
                _1721 = _1682 >= 15u;
            }
            else
            {
                _1721 = true;
            }
            if (_1721)
            {
                float4 _2382;
                do
                {
                    float4 _1734 = gowakuBackdrop.read(uint2(int3(int2(floor(gl_FragCoord.xy)), 0).xy), 0);
                    float4 _1736 = _1734 * (1.0 - _312);
                    if (_1701)
                    {
                        _2382 = _1680 + _1736;
                        break;
                    }
                    if (_1682 == 5u)
                    {
                        _2382 = (_1680 * _1734.w) + _1736;
                        break;
                    }
                    if (_1682 == 7u)
                    {
                        _2382 = (_1680 * (1.0 - _1734.w)) + _1736;
                        break;
                    }
                    if (_1682 == 10u)
                    {
                        _2382 = ((_1680 * (1.0 - _1734.w)) + (_1734 * _1680.w)) + _1736;
                        break;
                    }
                    float _1763 = _1680.w;
                    if (_1763 <= 0.0)
                    {
                        _2382 = float4(0.0);
                        break;
                    }
                    float3 _1770 = fast::clamp(_1680.xyz / float3(_1763), float3(0.0), float3(1.0));
                    float _1771 = _1734.w;
                    float3 _1780;
                    if (_1771 > 0.0)
                    {
                        _1780 = fast::clamp(_1734.xyz / float3(_1771), float3(0.0), float3(1.0));
                    }
                    else
                    {
                        _1780 = float3(0.0);
                    }
                    float3 _2373;
                    do
                    {
                        if (_1682 == 15u)
                        {
                            _2373 = _1780 * _1770;
                            break;
                        }
                        if (_1682 == 16u)
                        {
                            float _1792 = _1770.x;
                            float _1805;
                            if (_1780.x <= 0.5)
                            {
                                _1805 = (_1792 * 2.0) * _1780.x;
                            }
                            else
                            {
                                float _1799 = (2.0 * _1780.x) - 1.0;
                                _1805 = (_1792 + _1799) - (_1792 * _1799);
                            }
                            float _1806 = _1770.y;
                            float _1819;
                            if (_1780.y <= 0.5)
                            {
                                _1819 = (_1806 * 2.0) * _1780.y;
                            }
                            else
                            {
                                float _1813 = (2.0 * _1780.y) - 1.0;
                                _1819 = (_1806 + _1813) - (_1806 * _1813);
                            }
                            float _1820 = _1770.z;
                            float _1833;
                            if (_1780.z <= 0.5)
                            {
                                _1833 = (_1820 * 2.0) * _1780.z;
                            }
                            else
                            {
                                float _1827 = (2.0 * _1780.z) - 1.0;
                                _1833 = (_1820 + _1827) - (_1820 * _1827);
                            }
                            _2373 = float3(_1805, _1819, _1833);
                            break;
                        }
                        if (_1682 == 17u)
                        {
                            _2373 = precise::min(_1780, _1770);
                            break;
                        }
                        if (_1682 == 18u)
                        {
                            _2373 = precise::max(_1780, _1770);
                            break;
                        }
                        if (_1682 == 19u)
                        {
                            float _1847 = _1770.x;
                            float _1861;
                            do
                            {
                                if (_1780.x <= 0.0)
                                {
                                    _1861 = 0.0;
                                    break;
                                }
                                float _1860;
                                if (_1847 >= 1.0)
                                {
                                    _1860 = 1.0;
                                }
                                else
                                {
                                    _1860 = precise::min(1.0, _1780.x / (1.0 - _1847));
                                }
                                _1861 = _1860;
                                break;
                            } while(false);
                            float _1863 = _1770.y;
                            float _1877;
                            do
                            {
                                if (_1780.y <= 0.0)
                                {
                                    _1877 = 0.0;
                                    break;
                                }
                                float _1876;
                                if (_1863 >= 1.0)
                                {
                                    _1876 = 1.0;
                                }
                                else
                                {
                                    _1876 = precise::min(1.0, _1780.y / (1.0 - _1863));
                                }
                                _1877 = _1876;
                                break;
                            } while(false);
                            float _1879 = _1770.z;
                            float _1893;
                            do
                            {
                                if (_1780.z <= 0.0)
                                {
                                    _1893 = 0.0;
                                    break;
                                }
                                float _1892;
                                if (_1879 >= 1.0)
                                {
                                    _1892 = 1.0;
                                }
                                else
                                {
                                    _1892 = precise::min(1.0, _1780.z / (1.0 - _1879));
                                }
                                _1893 = _1892;
                                break;
                            } while(false);
                            _2373 = float3(_1861, _1877, _1893);
                            break;
                        }
                        if (_1682 == 20u)
                        {
                            float _1899 = _1770.x;
                            float _1914;
                            do
                            {
                                if (_1780.x >= 1.0)
                                {
                                    _1914 = 1.0;
                                    break;
                                }
                                float _1913;
                                if (_1899 <= 0.0)
                                {
                                    _1913 = 0.0;
                                }
                                else
                                {
                                    _1913 = 1.0 - precise::min(1.0, (1.0 - _1780.x) / _1899);
                                }
                                _1914 = _1913;
                                break;
                            } while(false);
                            float _1916 = _1770.y;
                            float _1931;
                            do
                            {
                                if (_1780.y >= 1.0)
                                {
                                    _1931 = 1.0;
                                    break;
                                }
                                float _1930;
                                if (_1916 <= 0.0)
                                {
                                    _1930 = 0.0;
                                }
                                else
                                {
                                    _1930 = 1.0 - precise::min(1.0, (1.0 - _1780.y) / _1916);
                                }
                                _1931 = _1930;
                                break;
                            } while(false);
                            float _1933 = _1770.z;
                            float _1948;
                            do
                            {
                                if (_1780.z >= 1.0)
                                {
                                    _1948 = 1.0;
                                    break;
                                }
                                float _1947;
                                if (_1933 <= 0.0)
                                {
                                    _1947 = 0.0;
                                }
                                else
                                {
                                    _1947 = 1.0 - precise::min(1.0, (1.0 - _1780.z) / _1933);
                                }
                                _1948 = _1947;
                                break;
                            } while(false);
                            _2373 = float3(_1914, _1931, _1948);
                            break;
                        }
                        if (_1682 == 21u)
                        {
                            float _1954 = _1770.x;
                            float _1966;
                            if (_1954 <= 0.5)
                            {
                                _1966 = (_1780.x * 2.0) * _1954;
                            }
                            else
                            {
                                float _1960 = (2.0 * _1954) - 1.0;
                                _1966 = (_1780.x + _1960) - (_1780.x * _1960);
                            }
                            float _1968 = _1770.y;
                            float _1980;
                            if (_1968 <= 0.5)
                            {
                                _1980 = (_1780.y * 2.0) * _1968;
                            }
                            else
                            {
                                float _1974 = (2.0 * _1968) - 1.0;
                                _1980 = (_1780.y + _1974) - (_1780.y * _1974);
                            }
                            float _1982 = _1770.z;
                            float _1994;
                            if (_1982 <= 0.5)
                            {
                                _1994 = (_1780.z * 2.0) * _1982;
                            }
                            else
                            {
                                float _1988 = (2.0 * _1982) - 1.0;
                                _1994 = (_1780.z + _1988) - (_1780.z * _1988);
                            }
                            _2373 = float3(_1966, _1980, _1994);
                            break;
                        }
                        if (_1682 == 22u)
                        {
                            float _2000 = _1770.x;
                            float _2028;
                            do
                            {
                                if (_2000 <= 0.5)
                                {
                                    _2028 = _1780.x - (((1.0 - (2.0 * _2000)) * _1780.x) * (1.0 - _1780.x));
                                    break;
                                }
                                float _2022;
                                if (_1780.x <= 0.25)
                                {
                                    _2022 = ((((16.0 * _1780.x) - 12.0) * _1780.x) + 4.0) * _1780.x;
                                }
                                else
                                {
                                    _2022 = sqrt(_1780.x);
                                }
                                _2028 = _1780.x + (((2.0 * _2000) - 1.0) * (_2022 - _1780.x));
                                break;
                            } while(false);
                            float _2030 = _1770.y;
                            float _2058;
                            do
                            {
                                if (_2030 <= 0.5)
                                {
                                    _2058 = _1780.y - (((1.0 - (2.0 * _2030)) * _1780.y) * (1.0 - _1780.y));
                                    break;
                                }
                                float _2052;
                                if (_1780.y <= 0.25)
                                {
                                    _2052 = ((((16.0 * _1780.y) - 12.0) * _1780.y) + 4.0) * _1780.y;
                                }
                                else
                                {
                                    _2052 = sqrt(_1780.y);
                                }
                                _2058 = _1780.y + (((2.0 * _2030) - 1.0) * (_2052 - _1780.y));
                                break;
                            } while(false);
                            float _2060 = _1770.z;
                            float _2088;
                            do
                            {
                                if (_2060 <= 0.5)
                                {
                                    _2088 = _1780.z - (((1.0 - (2.0 * _2060)) * _1780.z) * (1.0 - _1780.z));
                                    break;
                                }
                                float _2082;
                                if (_1780.z <= 0.25)
                                {
                                    _2082 = ((((16.0 * _1780.z) - 12.0) * _1780.z) + 4.0) * _1780.z;
                                }
                                else
                                {
                                    _2082 = sqrt(_1780.z);
                                }
                                _2088 = _1780.z + (((2.0 * _2060) - 1.0) * (_2082 - _1780.z));
                                break;
                            } while(false);
                            _2373 = float3(_2028, _2058, _2088);
                            break;
                        }
                        if (_1682 == 23u)
                        {
                            _2373 = abs(_1780 - _1770);
                            break;
                        }
                        if (_1682 == 24u)
                        {
                            _2373 = (_1780 + _1770) - ((_1780 * 2.0) * _1770);
                            break;
                        }
                        if (_1682 == 25u)
                        {
                            float _2113 = _1770.x;
                            float _2114 = _1770.y;
                            float _2116 = _1770.z;
                            float _2117 = precise::min(precise::min(_2113, _2114), _2116);
                            float _2119 = precise::max(precise::max(_2113, _2114), _2116);
                            float3 _2130;
                            if (_2119 > _2117)
                            {
                                _2130 = ((_1770 - float3(_2117)) * (precise::max(precise::max(_1780.x, _1780.y), _1780.z) - precise::min(precise::min(_1780.x, _1780.y), _1780.z))) / float3(_2119 - _2117);
                            }
                            else
                            {
                                _2130 = float3(0.0);
                            }
                            float3 _2146 = _2130 + float3((((0.300000011920928955078125 * _1780.x) + (0.589999973773956298828125 * _1780.y)) + (0.10999999940395355224609375 * _1780.z)) - (((0.300000011920928955078125 * _2130.x) + (0.589999973773956298828125 * _2130.y)) + (0.10999999940395355224609375 * _2130.z)));
                            float _2147 = _2146.x;
                            float _2149 = _2146.y;
                            float _2152 = _2146.z;
                            float _2154 = ((0.300000011920928955078125 * _2147) + (0.589999973773956298828125 * _2149)) + (0.10999999940395355224609375 * _2152);
                            float _2156 = precise::min(precise::min(_2147, _2149), _2152);
                            float _2158 = precise::max(precise::max(_2147, _2149), _2152);
                            float3 _2169;
                            if (_2156 < 0.0)
                            {
                                float3 _2162 = float3(_2154);
                                _2169 = _2162 + (((_2146 - _2162) * _2154) / float3(_2154 - _2156));
                            }
                            else
                            {
                                _2169 = _2146;
                            }
                            float3 _2181;
                            if (_2158 > 1.0)
                            {
                                float3 _2173 = float3(_2154);
                                _2181 = _2173 + (((_2169 - _2173) * (1.0 - _2154)) / float3(_2158 - _2154));
                            }
                            else
                            {
                                _2181 = _2169;
                            }
                            _2373 = _2181;
                            break;
                        }
                        if (_1682 == 26u)
                        {
                            float _2185 = _1770.x;
                            float _2186 = _1770.y;
                            float _2188 = _1770.z;
                            float _2197 = precise::min(precise::min(_1780.x, _1780.y), _1780.z);
                            float _2199 = precise::max(precise::max(_1780.x, _1780.y), _1780.z);
                            float3 _2210;
                            if (_2199 > _2197)
                            {
                                _2210 = ((_1780 - float3(_2197)) * (precise::max(precise::max(_2185, _2186), _2188) - precise::min(precise::min(_2185, _2186), _2188))) / float3(_2199 - _2197);
                            }
                            else
                            {
                                _2210 = float3(0.0);
                            }
                            float3 _2226 = _2210 + float3((((0.300000011920928955078125 * _1780.x) + (0.589999973773956298828125 * _1780.y)) + (0.10999999940395355224609375 * _1780.z)) - (((0.300000011920928955078125 * _2210.x) + (0.589999973773956298828125 * _2210.y)) + (0.10999999940395355224609375 * _2210.z)));
                            float _2227 = _2226.x;
                            float _2229 = _2226.y;
                            float _2232 = _2226.z;
                            float _2234 = ((0.300000011920928955078125 * _2227) + (0.589999973773956298828125 * _2229)) + (0.10999999940395355224609375 * _2232);
                            float _2236 = precise::min(precise::min(_2227, _2229), _2232);
                            float _2238 = precise::max(precise::max(_2227, _2229), _2232);
                            float3 _2249;
                            if (_2236 < 0.0)
                            {
                                float3 _2242 = float3(_2234);
                                _2249 = _2242 + (((_2226 - _2242) * _2234) / float3(_2234 - _2236));
                            }
                            else
                            {
                                _2249 = _2226;
                            }
                            float3 _2261;
                            if (_2238 > 1.0)
                            {
                                float3 _2253 = float3(_2234);
                                _2261 = _2253 + (((_2249 - _2253) * (1.0 - _2234)) / float3(_2238 - _2234));
                            }
                            else
                            {
                                _2261 = _2249;
                            }
                            _2373 = _2261;
                            break;
                        }
                        if (_1682 == 27u)
                        {
                            float3 _2283 = _1770 + float3((((0.300000011920928955078125 * _1780.x) + (0.589999973773956298828125 * _1780.y)) + (0.10999999940395355224609375 * _1780.z)) - (((0.300000011920928955078125 * _1770.x) + (0.589999973773956298828125 * _1770.y)) + (0.10999999940395355224609375 * _1770.z)));
                            float _2284 = _2283.x;
                            float _2286 = _2283.y;
                            float _2289 = _2283.z;
                            float _2291 = ((0.300000011920928955078125 * _2284) + (0.589999973773956298828125 * _2286)) + (0.10999999940395355224609375 * _2289);
                            float _2293 = precise::min(precise::min(_2284, _2286), _2289);
                            float _2295 = precise::max(precise::max(_2284, _2286), _2289);
                            float3 _2306;
                            if (_2293 < 0.0)
                            {
                                float3 _2299 = float3(_2291);
                                _2306 = _2299 + (((_2283 - _2299) * _2291) / float3(_2291 - _2293));
                            }
                            else
                            {
                                _2306 = _2283;
                            }
                            float3 _2318;
                            if (_2295 > 1.0)
                            {
                                float3 _2310 = float3(_2291);
                                _2318 = _2310 + (((_2306 - _2310) * (1.0 - _2291)) / float3(_2295 - _2291));
                            }
                            else
                            {
                                _2318 = _2306;
                            }
                            _2373 = _2318;
                            break;
                        }
                        float3 _2337 = _1780 + float3((((0.300000011920928955078125 * _1770.x) + (0.589999973773956298828125 * _1770.y)) + (0.10999999940395355224609375 * _1770.z)) - (((0.300000011920928955078125 * _1780.x) + (0.589999973773956298828125 * _1780.y)) + (0.10999999940395355224609375 * _1780.z)));
                        float _2338 = _2337.x;
                        float _2340 = _2337.y;
                        float _2343 = _2337.z;
                        float _2345 = ((0.300000011920928955078125 * _2338) + (0.589999973773956298828125 * _2340)) + (0.10999999940395355224609375 * _2343);
                        float _2347 = precise::min(precise::min(_2338, _2340), _2343);
                        float _2349 = precise::max(precise::max(_2338, _2340), _2343);
                        float3 _2360;
                        if (_2347 < 0.0)
                        {
                            float3 _2353 = float3(_2345);
                            _2360 = _2353 + (((_2337 - _2353) * _2345) / float3(_2345 - _2347));
                        }
                        else
                        {
                            _2360 = _2337;
                        }
                        float3 _2372;
                        if (_2349 > 1.0)
                        {
                            float3 _2364 = float3(_2345);
                            _2372 = _2364 + (((_2360 - _2364) * (1.0 - _2345)) / float3(_2349 - _2345));
                        }
                        else
                        {
                            _2372 = _2360;
                        }
                        _2373 = _2372;
                        break;
                    } while(false);
                    _2382 = float4(((_1770 * (1.0 - _1771)) + (fast::clamp(_2373, float3(0.0), float3(1.0)) * _1771)) * _1763, _1763);
                    break;
                } while(false);
                _2383 = _2382;
                break;
            }
            _2383 = _1680;
            break;
        } while(false);
        _2384 = _2383;
        break;
    } while(false);
    out.out_var_SV_Target = _2384;
    return out;
}

