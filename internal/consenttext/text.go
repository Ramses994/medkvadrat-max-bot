package consenttext

import (
	"fmt"
	"strings"
)

const (
	ServiceVersion = "service-v1"
	AdsVersion     = "ads-v1"
)

// ServiceText is the draft from 06 §2. The bracketed operator details are not approved.
const ServiceText = "Уведомления о записях в MAX\n\n" +
	"Я подтверждаю, что номер телефона, подтверждённый в MAX, принадлежит мне, и соглашаюсь " +
	"получать в мессенджере MAX от [наименование медицинской организации, ОГРН, ИНН] " +
	"служебные сообщения: о записи на приём, её изменении и отмене, напоминания о приёме и " +
	"просьбы подтвердить визит, а также сообщения о готовности результатов исследований.\n\n" +
	"Для этого клиника обрабатывает мои фамилию, имя, отчество, номер телефона, идентификатор " +
	"в MAX и сведения о записях на приём (дата, время, филиал, врач или вид приёма).\n\n" +
	"Согласие действует до отзыва. Отозвать его можно в боте кнопкой «Отключить уведомления» " +
	"или заявлением в клинику. Отказ не влияет на оказание медицинской помощи."

// AdsText is the draft from 06 §3. The name and the link are not approved.
const AdsText = "Акции и предложения клиники\n\n" +
	"Хочу получать в MAX информацию о медицинских услугах, акциях, спецпредложениях и " +
	"программах лояльности [наименование]. Полный текст согласия — [ссылка].\n" +
	"Отписаться можно в любой момент кнопкой «Отписаться от акций»."

// Check refuses to start when a switched-on consent still contains an unapproved template.
func Check(serviceEnabled, adsEnabled bool, serviceVersion, adsVersion string) error {
	if serviceEnabled {
		if err := checkOne("BOT_CONSENT_ENABLED", serviceVersion, ServiceVersion, ServiceText); err != nil {
			return err
		}
	}
	if adsEnabled {
		if err := checkOne("BOT_CONSENT_ADS_ENABLED", adsVersion, AdsVersion, AdsText); err != nil {
			return err
		}
	}
	return nil
}

func checkOne(flag, got, want, text string) error {
	if got != want {
		return fmt.Errorf("%s: нет текста для версии %s", flag, got)
	}
	if strings.Contains(text, "[") && strings.Contains(text, "]") {
		return fmt.Errorf("%s: в тексте %s остался шаблон [ … ], бот не запущен", flag, got)
	}
	return nil
}
