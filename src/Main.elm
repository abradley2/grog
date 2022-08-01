port module Main exposing (..)

import Browser exposing (element)
import Html as H exposing (Html)
import Json.Decode as Decode exposing (Error)
import Json.Encode as Encode exposing (Value)


port onMessage : (Value -> msg) -> Sub msg


port sendMessage : String -> Cmd msg


type Msg
    = OnMessage Value


type alias Model =
    { lastMessage : Maybe (Result Error String)
    }


init : Value -> ( Model, Cmd Msg )
init =
    let
        model : Model
        model =
            { lastMessage = Nothing
            }
    in
    always
        ( model
        , sendMessage "0"
        )


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        OnMessage val ->
            ( { model
                | lastMessage = Just <| Decode.decodeValue Decode.string val
              }
            , Cmd.none
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
