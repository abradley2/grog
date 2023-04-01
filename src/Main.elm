port module Main exposing (..)

import Browser exposing (element)
import Browser.Dom as Dom exposing (Viewport)
import Html as H exposing (Html)
import Html.Attributes as A
import Html.Events as E
import Html.Keyed as Keyed
import Json.Decode as Decode exposing (Decoder)
import Json.Encode exposing (Value)
import Maybe.Extra as MaybeX
import Parser exposing ((|.), (|=), Parser)
import Set
import Task


port onMessage : (String -> msg) -> Sub msg


port sendMessage : String -> Cmd msg


type Effect
    = EffectNone
    | EffectBatch (List Effect)
    | EffectSetCursor Int
    | EffectScrollToBottom String


perform : Effect -> Cmd Msg
perform effect =
    case effect of
        EffectNone ->
            Cmd.none

        EffectBatch effects ->
            Cmd.batch (List.map perform effects)

        EffectSetCursor cursor ->
            sendMessage (String.fromInt cursor)

        EffectScrollToBottom id ->
            Dom.getViewportOf id
                |> Task.andThen
                    (\element ->
                        let
                            height =
                                element.scene.height
                        in
                        Dom.setViewportOf id 0 height
                    )
                |> Task.attempt UpdatedViewport


type Msg
    = OnMessage String
    | ReceivedViewport (Result Dom.Error Viewport)
    | UpdatedViewport (Result Dom.Error ())
    | LogsScrolled


type alias Model =
    { logs : Maybe (Result Decode.Error (List Log))
    , mode : Mode
    , autoScrolling : Bool
    }


type Mode
    = Play
    | BrowseAt Int


type alias Log =
    { id : Int
    , content : String
    }


logId : Log -> String
logId =
    .id >> String.fromInt >> (++) "log-"


parseDecoder : String -> Parser a -> Decoder a
parseDecoder name parser =
    Decode.andThen
        (\val ->
            case Parser.run parser val of
                Err _ ->
                    Decode.fail <| val ++ " is not a valid " ++ name

                Ok parsed ->
                    Decode.succeed parsed
        )
        Decode.string


logParser : Parser Log
logParser =
    Parser.succeed
        Log
        |= Parser.int
        |. Parser.token ":"
        |= Parser.variable
            { start = always True
            , inner = always True
            , reserved = Set.empty
            }
        |. Parser.end


init : Value -> ( Model, Effect )
init =
    let
        model : Model
        model =
            { logs = Nothing
            , mode = Play
            , autoScrolling = False
            }
    in
    always
        ( model
        , EffectNone
        )


update : Msg -> Model -> ( Model, Effect )
update msg model =
    case msg of
        LogsScrolled ->
            let
                l =
                    Debug.log "LOGS SCROLLED" <| not model.autoScrolling
            in
            ( model, EffectNone )

        ReceivedViewport _ ->
            ( model, EffectNone )

        UpdatedViewport (Err _) ->
            ( { model | autoScrolling = False }, EffectNone )

        UpdatedViewport (Ok _) ->
            ( { model | autoScrolling = False }, EffectNone )

        OnMessage val ->
            let
                logs : Result Decode.Error (List Log)
                logs =
                    Decode.decodeString (Decode.list (parseDecoder "log" logParser)) val

                nextCursor =
                    case model.mode of
                        Play ->
                            Result.map (List.map .id >> List.maximum) logs
                                |> Result.toMaybe
                                |> MaybeX.join

                        BrowseAt cursor ->
                            Just cursor

                scrollEffect : Maybe Effect
                scrollEffect =
                    case model.mode of
                        Play ->
                            Just <| EffectScrollToBottom logItemListId

                        _ ->
                            Nothing
            in
            ( { model
                | logs = Just logs
                , autoScrolling = MaybeX.isJust scrollEffect
              }
            , EffectBatch
                [ Maybe.map EffectSetCursor nextCursor |> Maybe.withDefault EffectNone
                , Maybe.withDefault EffectNone scrollEffect
                ]
            )


view : Model -> Html Msg
view model =
    H.div
        [ A.class "flex flex-column w-100 vh-100 overflow-x-auto overflow-y-auto avenir" ]
        [ toolbar model
        , Maybe.map Result.toMaybe model.logs
            |> MaybeX.join
            |> Maybe.map (logItemList model)
            |> Maybe.withDefault (H.text "")
        ]


toolbar : Model -> Html Msg
toolbar _ =
    H.div
        []
        [ H.text "" ]


logItemListId : String
logItemListId =
    "log-item-list"


logItemList : Model -> List Log -> Html Msg
logItemList model =
    List.map logItem
        >> Keyed.ul [ A.class "list pa0" ]
        >> List.singleton
        >> H.div
            [ A.class "overflow-x-scroll overflow-y-scroll"
            , A.style "max-height" "64rem"
            , A.id logItemListId
            , E.on "scroll" <|
                if model.autoScrolling then
                    Decode.fail ""

                else
                    Decode.succeed LogsScrolled
            ]


logItem : Log -> ( String, Html Msg )
logItem log =
    let
        key : String
        key =
            logId log
    in
    ( key
    , H.li
        [ A.id key
        , A.classList
            [ ( "flex h3 pa1 f7 items-center", True )
            , case modBy 2 log.id of
                0 ->
                    ( "bg-washed-green", True )

                _ ->
                    ( "bg-light-green", True )
            ]
        ]
        [ H.pre
            [ A.class "courier"
            ]
            [ H.text log.content ]
        ]
    )


main : Program Value Model Msg
main =
    element
        { init = init >> Tuple.mapSecond perform
        , update = \msg -> update msg >> Tuple.mapSecond perform
        , view = view
        , subscriptions = always (onMessage OnMessage)
        }
