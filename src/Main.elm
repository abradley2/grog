port module Main exposing (..)

import Browser exposing (element)
import Html as H exposing (Html)
import Json.Decode as Decode exposing (Decoder, Error)
import Json.Encode as Encode exposing (Value)
import Maybe.Extra as MaybeX
import Parser exposing ((|.), (|=), Parser)
import Set
import Stat exposing (median)


port onMessage : (String -> msg) -> Sub msg


port sendMessage : String -> Cmd msg


type Msg
    = OnMessage String


type alias Model =
    { logs : Maybe (Result Error (List Message))
    , cursor : Maybe Int
    }


type alias Message =
    { cursor : Int
    , content : String
    }


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


messageParser : Parser Message
messageParser =
    Parser.succeed
        Message
        |= Parser.int
        |. Parser.token ":"
        |= Parser.variable
            { start = always True
            , inner = always True
            , reserved = Set.empty
            }
        |. Parser.end


init : Value -> ( Model, Cmd Msg )
init =
    let
        model : Model
        model =
            { logs = Nothing
            , cursor = Nothing
            }
    in
    always
        ( model
        , Cmd.none
        )


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        OnMessage val ->
            let
                logs =
                    Decode.decodeString (Decode.list (parseDecoder "message" messageParser)) val

                nextCursor =
                    Result.map (List.map (.cursor >> toFloat) >> median >> Maybe.map (floor >> (+) 1)) logs
                        |> Result.toMaybe
                        |> MaybeX.join
            in
            ( { model
                | logs = Just logs
                , cursor = nextCursor
              }
            , Maybe.map (String.fromInt >> sendMessage) nextCursor |> Maybe.withDefault Cmd.none
            )


view : Model -> Html Msg
view model =
    H.div
        []
        [ H.text "" ]


main : Program Value Model Msg
main =
    element
        { init = init
        , update = update
        , view = view
        , subscriptions = always (onMessage OnMessage)
        }
